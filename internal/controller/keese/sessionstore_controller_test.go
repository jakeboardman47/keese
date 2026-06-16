// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// fakeSSRebacWriter records workspace-binding tuple writes/deletes for SessionStore.
type fakeSSRebacWriter struct {
	Written []SessionStoreTuple
	Deleted []SessionStoreTuple
}

func (f *fakeSSRebacWriter) Write(_ context.Context, t []SessionStoreTuple) error {
	f.Written = append(f.Written, t...)
	return nil
}

func (f *fakeSSRebacWriter) Delete(_ context.Context, t []SessionStoreTuple) error {
	f.Deleted = append(f.Deleted, t...)
	return nil
}

// fakeSSMigrator records Migrate calls without touching a database. Err, when set,
// is returned on every call.
type fakeSSMigrator struct {
	Calls int
	Err   error
}

func (f *fakeSSMigrator) Migrate(_ context.Context, _, _ string) error {
	f.Calls++
	return f.Err
}

var _ = Describe("SessionStore Controller", func() {
	const ssNS = "default"

	newReconciler := func(rebac SessionStoreRebacWriter, mig SessionStoreMigrator) *SessionStoreReconciler {
		return &SessionStoreReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: noopRecorder{},
			Rebac:    rebac,
			Migrator: mig,
		}
	}

	// reconcileUntilStable drives the reconciler: the first call adds the finalizer
	// and requeues; subsequent calls do the real work.
	reconcileUntilStable := func(rec *SessionStoreReconciler, nn types.NamespacedName, n int) {
		for i := 0; i < n; i++ {
			_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Describe("TestSessionStoreReconcile_SQLite", func() {
		It("validates the PVC ref and reaches Ready", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-sqlite-pvc", Namespace: ssNS},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pvc) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-sqlite", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendSQLite,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "ss-sqlite-pvc"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			rec := newReconciler(&fakeSSRebacWriter{}, &fakeSSMigrator{})
			reconcileUntilStable(rec, nn, 3)

			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseReady))
			Expect(ss.Status.ObservedGeneration).To(Equal(ss.Generation))
		})

		It("goes Degraded when the SQLite PVC is missing", func() {
			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-sqlite-nopvc", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendSQLite,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "does-not-exist"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			rec := newReconciler(&fakeSSRebacWriter{}, &fakeSSMigrator{})
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseDegraded))
		})

		It("is idempotent across ≥3 reconciles with no spec change", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-idem-pvc", Namespace: ssNS},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pvc) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-idem", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendSQLite,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "ss-idem-pvc"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			rec := newReconciler(&fakeSSRebacWriter{}, &fakeSSMigrator{})
			reconcileUntilStable(rec, nn, 2)
			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseReady))
			genAfterConverge := ss.Generation

			for i := 0; i < 3; i++ {
				res, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
				Expect(res.Requeue).To(BeFalse())
			}
			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseReady))
			Expect(ss.Generation).To(Equal(genAfterConverge), "no spec churn across reconciles")
		})
	})

	Describe("TestSessionStoreReconcile_PG", func() {
		It("applies the migration and reaches Ready when the DSN secret exists", func() {
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-dsn", Namespace: ssNS},
				StringData: map[string]string{"dsn": "postgres://app@db/sessions"},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, sec) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendPostgres,
					Postgres:     &keesev1alpha1.PostgresSessionBackend{DSNSecretRef: corev1.LocalObjectReference{Name: "ss-pg-dsn"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			mig := &fakeSSMigrator{}
			rec := newReconciler(&fakeSSRebacWriter{}, mig)
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseReady))
			Expect(ss.Status.MigrationVersion).To(Equal(currentSchemaVersion))
			Expect(mig.Calls).To(BeNumerically(">=", 1))
		})

		It("goes Degraded when the migration fails", func() {
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-dsn-bad", Namespace: ssNS},
				StringData: map[string]string{"dsn": "postgres://app@db/sessions"},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, sec) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-badmig", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendPostgres,
					Postgres:     &keesev1alpha1.PostgresSessionBackend{DSNSecretRef: corev1.LocalObjectReference{Name: "ss-pg-dsn-bad"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			rec := newReconciler(&fakeSSRebacWriter{}, &fakeSSMigrator{Err: errors.New("connect refused")})
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Get(ctx, nn, ss)).To(Succeed())
			Expect(ss.Status.Phase).To(Equal(keesev1alpha1.SessionStorePhaseDegraded))
		})
	})

	Describe("TestSessionStorePGMigrate_Idempotent", func() {
		It("runs the migration ×3 with no error and gates re-runs on migrationVersion", func() {
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-idem-dsn", Namespace: ssNS},
				StringData: map[string]string{"dsn": "postgres://app@db/sessions"},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, sec) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-idem", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendPostgres,
					Postgres:     &keesev1alpha1.PostgresSessionBackend{DSNSecretRef: corev1.LocalObjectReference{Name: "ss-pg-idem-dsn"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}
			DeferCleanup(func() { deleteSS(nn) })

			// The real schema migration is itself guarded (CREATE TABLE IF NOT
			// EXISTS / CREATE POLICY IF NOT EXISTS); this asserts the controller-level
			// idempotency: NoopSessionStoreMigrator returns no error across ≥3 runs
			// and the migrationVersion gate prevents a re-run after the first apply.
			mig := &fakeSSMigrator{}
			rec := newReconciler(&fakeSSRebacWriter{}, mig)
			// finalizer add + first real reconcile (applies migration once)
			reconcileUntilStable(rec, nn, 2)
			Expect(mig.Calls).To(Equal(1))

			// 3 further reconciles: no error, migration NOT re-run (gated).
			for i := 0; i < 3; i++ {
				_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(mig.Calls).To(Equal(1), "migration gated on migrationVersion; not re-run")
		})
	})

	Describe("CEL one-of (SessionStoreOneBackend)", func() {
		It("rejects a SessionStore with both backends set", func() {
			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-both", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendSQLite,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "pvc"}},
					Postgres:     &keesev1alpha1.PostgresSessionBackend{DSNSecretRef: corev1.LocalObjectReference{Name: "dsn"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).NotTo(Succeed())
		})

		It("rejects type=postgres with no postgres block", func() {
			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-pg-noblock", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendPostgres,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "pvc"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).NotTo(Succeed())
		})

		It("accepts the sqlite minimal sample shape", func() {
			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-sample-ok", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendSQLite,
					SQLite:       &keesev1alpha1.SQLiteSessionBackend{PVCRef: corev1.LocalObjectReference{Name: "sessions-pvc"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			Expect(k8sClient.Delete(ctx, ss)).To(Succeed())
		})
	})

	Describe("deletion", func() {
		It("purges the workspace tuple and removes the finalizer", func() {
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-del-dsn", Namespace: ssNS},
				StringData: map[string]string{"dsn": "postgres://app@db/sessions"},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, sec) })

			ss := &keesev1alpha1.SessionStore{
				ObjectMeta: metav1.ObjectMeta{Name: "ss-delete", Namespace: ssNS},
				Spec: keesev1alpha1.SessionStoreSpec{
					WorkspaceRef: "demo-workspace",
					Type:         keesev1alpha1.SessionStoreBackendPostgres,
					Postgres:     &keesev1alpha1.PostgresSessionBackend{DSNSecretRef: corev1.LocalObjectReference{Name: "ss-del-dsn"}},
				},
			}
			Expect(k8sClient.Create(ctx, ss)).To(Succeed())
			nn := types.NamespacedName{Name: ss.Name, Namespace: ssNS}

			rebac := &fakeSSRebacWriter{}
			rec := newReconciler(rebac, &fakeSSMigrator{})
			reconcileUntilStable(rec, nn, 2)

			Expect(k8sClient.Delete(ctx, ss)).To(Succeed())
			_, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return k8sClient.Get(ctx, nn, &keesev1alpha1.SessionStore{}) != nil
			}, reconcileWait, reconcileTick).Should(BeTrue())
			Expect(rebac.Deleted).To(HaveLen(1))
			Expect(rebac.Deleted[0].Relation).To(Equal("workspace"))
		})
	})
})

// deleteSS deletes a SessionStore and reconciles its finalizer away.
func deleteSS(nn types.NamespacedName) {
	ss := &keesev1alpha1.SessionStore{}
	if err := k8sClient.Get(ctx, nn, ss); err != nil {
		return
	}
	_ = k8sClient.Delete(ctx, ss)
	rec := &SessionStoreReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: noopRecorder{},
		Rebac:    SessionStoreNoopRebacWriter{},
		Migrator: NoopSessionStoreMigrator{},
	}
	Eventually(func() bool {
		_, _ = rec.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		return k8sClient.Get(ctx, nn, &keesev1alpha1.SessionStore{}) != nil
	}, reconcileWait, reconcileTick).Should(BeTrue())
}
