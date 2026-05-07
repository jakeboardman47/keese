// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
)

const (
	featureGateFieldOwner = "keese-feature-gate-controller"

	// FeatureGateConfigMapName is the projection target. Every keese
	// binary mounts this ConfigMap at /etc/keese/features/gates.json.
	FeatureGateConfigMapName = "keese-features"

	// FeatureGateNamespace is the canonical namespace for the
	// projection (matches the operator deployment).
	FeatureGateNamespace = "keese-system"

	// FeatureGateConfigMapKey is the JSON key inside the CM.
	FeatureGateConfigMapKey = "gates.json"
)

// FeatureGateReconciler watches FeatureGate CRs and projects their
// effective values into ConfigMap keese-system/keese-features. See
// design 27 §2 + §6.
type FeatureGateReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Namespace overrides the projection target namespace (tests).
	Namespace string
}

// +kubebuilder:rbac:groups=policy.keese.ai,resources=featuregates,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=policy.keese.ai,resources=featuregates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;patch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile pulls the full FeatureGate list (cluster-scoped, low
// cardinality) and rewrites the projection ConfigMap from scratch.
// This is intentionally simpler than per-CR diffing: O(N) where N
// is the number of gates (small), and avoids races where a deleted
// CR leaves a stale entry in the CM.
func (r *FeatureGateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("featuregate", req.Name)

	var list policyv1alpha1.FeatureGateList
	if err := r.List(ctx, &list); err != nil {
		return ctrl.Result{}, fmt.Errorf("list FeatureGates: %w", err)
	}

	projection := make(map[string]bool, len(list.Items))
	for i := range list.Items {
		fg := &list.Items[i]
		eff := effectiveValue(fg)
		projection[fg.Name] = eff
		if err := r.patchStatus(ctx, fg, eff); err != nil {
			log.Error(err, "patch status", "name", fg.Name)
		}
	}

	if err := r.applyProjection(ctx, projection); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply projection: %w", err)
	}

	// Emit a RestartRequired event on every gate that was just
	// transitioned and carries spec.restartRequired=true. We use
	// status.lastTransitionTime as a "did it just change" proxy.
	for i := range list.Items {
		fg := &list.Items[i]
		if !fg.Spec.RestartRequired {
			continue
		}
		if fg.Status.LastTransitionTime == nil ||
			time.Since(fg.Status.LastTransitionTime.Time) > 30*time.Second {
			continue
		}
		r.Recorder.Eventf(fg, corev1.EventTypeNormal,
			"RestartRequired",
			"FeatureGate %q changed; consumers (%s) require restart to observe the new value",
			fg.Name, joinSorted(fg.Spec.Owners))
	}

	return ctrl.Result{}, nil
}

// effectiveValue returns spec.override ?? DefaultEffective(stage),
// matching the design's projection rule.
func effectiveValue(fg *policyv1alpha1.FeatureGate) bool {
	if fg.Spec.Override != nil {
		return *fg.Spec.Override
	}
	return policyv1alpha1.DefaultEffective(fg.Spec.Stage)
}

// patchStatus updates status.effective + status.observedGeneration
// + status.lastTransitionTime + the Ready condition. Idempotent —
// no-ops when nothing changed.
func (r *FeatureGateReconciler) patchStatus(
	ctx context.Context, fg *policyv1alpha1.FeatureGate, eff bool,
) error {
	orig := fg.DeepCopy()
	changed := false

	if fg.Status.Effective != eff {
		fg.Status.Effective = eff
		now := metav1.NewTime(time.Now().UTC())
		fg.Status.LastTransitionTime = &now
		changed = true
	}
	if fg.Status.ObservedGeneration != fg.Generation {
		fg.Status.ObservedGeneration = fg.Generation
		changed = true
	}

	condChanged := upsertReadyCondition(&fg.Status.Conditions, fg.Generation)
	changed = changed || condChanged

	if !changed {
		return nil
	}
	return r.Status().Patch(ctx, fg, client.MergeFrom(orig))
}

// applyProjection SSA-writes the canonical map into the CM. Reads
// the CM first so the unchanged path is a no-op (no SSA conflict
// noise).
func (r *FeatureGateReconciler) applyProjection(
	ctx context.Context, projection map[string]bool,
) error {
	ns := r.Namespace
	if ns == "" {
		ns = FeatureGateNamespace
	}
	body, err := json.MarshalIndent(projection, "", "  ")
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      FeatureGateConfigMapName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": featureGateFieldOwner,
				"app.kubernetes.io/part-of":    "keese",
			},
		},
		Data: map[string]string{
			FeatureGateConfigMapKey: string(body),
		},
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(ctx,
		types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace},
		existing)
	switch {
	case errors.IsNotFound(err):
		return r.Create(ctx, cm,
			client.FieldOwner(featureGateFieldOwner))
	case err != nil:
		return err
	}
	if existing.Data[FeatureGateConfigMapKey] == cm.Data[FeatureGateConfigMapKey] {
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Data = cm.Data
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range cm.Labels {
		existing.Labels[k] = v
	}
	return r.Patch(ctx, existing, patch,
		client.FieldOwner(featureGateFieldOwner))
}

func upsertReadyCondition(conds *[]metav1.Condition, gen int64) bool {
	now := metav1.NewTime(time.Now().UTC())
	target := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Projected",
		Message:            "feature-gate effective value projected to keese-features ConfigMap",
		ObservedGeneration: gen,
		LastTransitionTime: now,
	}
	for i := range *conds {
		if (*conds)[i].Type == target.Type {
			c := &(*conds)[i]
			if c.Status == target.Status &&
				c.Reason == target.Reason &&
				c.ObservedGeneration == gen {
				return false
			}
			c.Status = target.Status
			c.Reason = target.Reason
			c.Message = target.Message
			c.ObservedGeneration = gen
			c.LastTransitionTime = now
			return true
		}
	}
	*conds = append(*conds, target)
	return true
}

func joinSorted(in []string) string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	s := ""
	for i, v := range out {
		if i > 0 {
			s += ","
		}
		s += v
	}
	return s
}

// SetupWithManager sets up the controller with the Manager.
func (r *FeatureGateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("feature-gate-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.FeatureGate{}).
		Named("policy-feature-gate").
		Complete(r)
}
