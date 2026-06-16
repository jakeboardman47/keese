// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	// sessionStoreFinalizer purges the workspace-binding ReBAC tuple before the
	// object is removed (rule 04.10 — finalizer for externally-allocated state).
	sessionStoreFinalizer = "finalizers.sessionstore.keese.ai/cleanup"
	// sessionStoreFieldOwner is the Server-Side-Apply field owner for any child
	// object this controller writes (rule 04.7).
	sessionStoreFieldOwner = "keese-sessionstore-controller"
)

// SessionStoreReconciler reconciles a SessionStore object.
//
// +kubebuilder:rbac:groups=keese.ai,resources=sessionstores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=sessionstores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=sessionstores/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
type SessionStoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    SessionStoreRebacWriter
	Migrator SessionStoreMigrator
}

// Reconcile implements the SessionStore reconciliation loop.
//
// FSM: Pending → (Migrating for postgres) → Ready ↔ Degraded; Terminating on deletion.
//
// Idempotency guarantee: converges in ≤3 reconciles with no spec change
// (rule 04.16). Status is never read into the next reconcile decision (rule 04.4)
// except for the migrationVersion gate, which is a record of externally-applied
// schema state (not this controller's own prior phase) — re-running the migration
// is itself idempotent, so the gate is an optimization, not a correctness crutch.
func (r *SessionStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ss := &keesev1alpha1.SessionStore{}
	if err := r.Get(ctx, req.NamespacedName, ss); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SessionStore: %w", err)
	}

	orig := ss.DeepCopy()

	// Deletion path — purge ReBAC tuples before finalizer removal.
	if !ss.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, ss, orig)
	}

	// Ensure finalizer before any external (ReBAC / migration) work.
	if !controllerutil.ContainsFinalizer(ss, sessionStoreFinalizer) {
		controllerutil.AddFinalizer(ss, sessionStoreFinalizer)
		if err := r.Update(ctx, ss); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	r.Recorder.Eventf(ss, corev1.EventTypeNormal, ReasonSessionStoreValidated, "spec validated")

	// Validate the active backend reference exists. Backend selection is the
	// discriminated one-of; the CEL XValidation already guaranteed exactly one is
	// set, so here we only confirm the referenced resource exists.
	if degraded, res, err := r.validateBackend(ctx, ss, orig); degraded {
		return res, err
	}

	// Record the workspace-binding ReBAC tuple. Deferred to a no-op writer until
	// the OpenFGA model gains a `sessionstore` type (see sessionstore_rebac.go).
	tuple := SessionStoreWorkspaceTuple(string(ss.UID), ss.Spec.WorkspaceRef)
	if err := r.Rebac.Write(ctx, []SessionStoreTuple{tuple}); err != nil {
		log.Error(err, "rebac write failed")
		r.Recorder.Eventf(ss, corev1.EventTypeWarning, ReasonSessionStoreRebacSyncFailed, "rebac sync failed: %v", err)
		return r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseDegraded,
			ssReadyCond(metav1.ConditionFalse, ReasonSessionStoreRebacSyncFailed, err.Error()))
	}
	r.Recorder.Eventf(ss, corev1.EventTypeNormal, ReasonSessionStoreRebacSyncSucceeded, "workspace binding tuple synced")

	// Postgres backend: apply the idempotent schema + RLS migration, gated on
	// status.migrationVersion so it is not re-run once current.
	if ss.Spec.Type == keesev1alpha1.SessionStoreBackendPostgres {
		if ss.Status.MigrationVersion != currentSchemaVersion {
			ss.Status.Phase = keesev1alpha1.SessionStorePhaseMigrating
			r.Recorder.Eventf(ss, corev1.EventTypeNormal, ReasonSessionStoreMigrating, "applying schema %s", currentSchemaVersion)
			pg := ss.Spec.Postgres
			if err := r.Migrator.Migrate(ctx, pg.DSNSecretRef.Name, pg.SSLMode); err != nil {
				log.Error(err, "pg migration failed")
				r.Recorder.Eventf(ss, corev1.EventTypeWarning, ReasonSessionStoreMigrationFailed, "migration failed: %v", err)
				return r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseDegraded,
					ssReadyCond(metav1.ConditionFalse, ReasonSessionStoreMigrationFailed, err.Error()))
			}
			ss.Status.MigrationVersion = currentSchemaVersion
			r.Recorder.Eventf(ss, corev1.EventTypeNormal, ReasonSessionStoreMigrated, "schema %s applied", currentSchemaVersion)
		}
	}

	// Happy path — backend valid, migration (if any) applied.
	r.Recorder.Eventf(ss, corev1.EventTypeNormal, ReasonSessionStoreReady, "session store ready")
	return r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseReady,
		ssReadyCond(metav1.ConditionTrue, ReasonSessionStoreReady, "session store backend ready"))
}

// validateBackend confirms the referenced backend resource exists. Returns
// (degraded, result, err): when degraded is true the caller returns result
// directly (status already patched).
func (r *SessionStoreReconciler) validateBackend(
	ctx context.Context,
	ss, orig *keesev1alpha1.SessionStore,
) (bool, ctrl.Result, error) {
	switch ss.Spec.Type {
	case keesev1alpha1.SessionStoreBackendSQLite:
		pvc := &corev1.PersistentVolumeClaim{}
		key := client.ObjectKey{Namespace: ss.Namespace, Name: ss.Spec.SQLite.PVCRef.Name}
		if err := r.Get(ctx, key, pvc); err != nil {
			msg := fmt.Sprintf("sqlite PVC %q not found", ss.Spec.SQLite.PVCRef.Name)
			r.Recorder.Eventf(ss, corev1.EventTypeWarning, ReasonSessionStoreDegraded, "%s", msg)
			res, derr := r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseDegraded,
				ssReadyCond(metav1.ConditionFalse, ReasonSessionStoreDegraded, msg))
			return true, res, derr
		}
	case keesev1alpha1.SessionStoreBackendPostgres:
		sec := &corev1.Secret{}
		key := client.ObjectKey{Namespace: ss.Namespace, Name: ss.Spec.Postgres.DSNSecretRef.Name}
		if err := r.Get(ctx, key, sec); err != nil {
			msg := fmt.Sprintf("postgres DSN secret %q not found", ss.Spec.Postgres.DSNSecretRef.Name)
			r.Recorder.Eventf(ss, corev1.EventTypeWarning, ReasonSessionStoreDegraded, "%s", msg)
			res, derr := r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseDegraded,
				ssReadyCond(metav1.ConditionFalse, ReasonSessionStoreDegraded, msg))
			return true, res, derr
		}
	}
	return false, ctrl.Result{}, nil
}

// reconcileDelete purges the workspace-binding tuple, then removes the finalizer.
func (r *SessionStoreReconciler) reconcileDelete(ctx context.Context, ss, orig *keesev1alpha1.SessionStore) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(ss, sessionStoreFinalizer) {
		return ctrl.Result{}, nil
	}

	ss.Status.Phase = keesev1alpha1.SessionStorePhaseTerminating

	tuple := SessionStoreWorkspaceTuple(string(ss.UID), ss.Spec.WorkspaceRef)
	if err := r.Rebac.Delete(ctx, []SessionStoreTuple{tuple}); err != nil {
		log.Error(err, "rebac purge failed")
		r.Recorder.Eventf(ss, corev1.EventTypeWarning, ReasonSessionStoreRebacPurgeFailed, "rebac purge failed: %v", err)
		return r.setStatus(ctx, ss, orig, keesev1alpha1.SessionStorePhaseTerminating,
			ssReadyCond(metav1.ConditionFalse, ReasonSessionStoreRebacPurgeFailed, err.Error()))
	}

	controllerutil.RemoveFinalizer(ss, sessionStoreFinalizer)
	if err := r.Update(ctx, ss); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// setStatus patches the status subresource (MergeFrom orig). Never reads status
// into the next reconcile decision (rule 04.4).
func (r *SessionStoreReconciler) setStatus(
	ctx context.Context,
	ss, orig *keesev1alpha1.SessionStore,
	phase keesev1alpha1.SessionStorePhase,
	cond metav1.Condition,
) (ctrl.Result, error) {
	ss.Status.Phase = phase
	ss.Status.ObservedGeneration = ss.Generation
	cond.ObservedGeneration = ss.Generation
	meta.SetStatusCondition(&ss.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, ss, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}
	return ctrl.Result{}, nil
}

// ssReadyCond builds a Ready condition. ObservedGeneration is stamped by setStatus.
func ssReadyCond(status metav1.ConditionStatus, reason, msg string) metav1.Condition {
	return metav1.Condition{
		Type:    keesev1alpha1.SessionStoreConditionReady,
		Status:  status,
		Reason:  reason,
		Message: msg,
	}
}

// SetupWithManager wires the controller into the manager, defaulting dependencies.
func (r *SessionStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("sessionstore-controller")
	}
	if r.Rebac == nil {
		// Deferred: no `sessionstore` type in the OpenFGA model yet (rebac-modeler).
		r.Rebac = SessionStoreNoopRebacWriter{}
	}
	if r.Migrator == nil {
		r.Migrator = NoopSessionStoreMigrator{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.SessionStore{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("sessionstore").
		Complete(r)
}
