// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
)

func mkFG(name string, stage policyv1alpha1.FeatureGateStage, override *bool) *policyv1alpha1.FeatureGate {
	return &policyv1alpha1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec: policyv1alpha1.FeatureGateSpec{
			Description: "test",
			Stage:       stage,
			Override:    override,
		},
	}
}

func ptrBool(b bool) *bool { return &b }

func newReconciler(t *testing.T, objs ...runtime.Object) *FeatureGateReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := policyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("policy add: %v", err)
	}
	clientObjs := []client.Object{}
	for _, o := range objs {
		clientObjs = append(clientObjs, o.(client.Object))
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clientObjs...).
		WithStatusSubresource(&policyv1alpha1.FeatureGate{}).
		Build()
	return &FeatureGateReconciler{
		Client:    c,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(8),
		Namespace: "test-ns",
	}
}

func TestReconcile_ProjectsEffectiveValue(t *testing.T) {
	fg := mkFG("cosign-installplan-verify", policyv1alpha1.FeatureGateStageAlpha, ptrBool(true))
	r := newReconciler(t, fg)

	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), types.NamespacedName{
		Name: FeatureGateConfigMapName, Namespace: "test-ns",
	}, cm); err != nil {
		t.Fatalf("get CM: %v", err)
	}
	got := map[string]bool{}
	if err := json.Unmarshal([]byte(cm.Data[FeatureGateConfigMapKey]), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got["cosign-installplan-verify"] {
		t.Errorf("expected projection true; got %v", got)
	}
}

func TestReconcile_AlphaWithoutOverrideDefaultsOff(t *testing.T) {
	fg := mkFG("alpha-default-off", policyv1alpha1.FeatureGateStageAlpha, nil)
	r := newReconciler(t, fg)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	cm := &corev1.ConfigMap{}
	_ = r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm)
	got := map[string]bool{}
	_ = json.Unmarshal([]byte(cm.Data[FeatureGateConfigMapKey]), &got)
	if got["alpha-default-off"] {
		t.Errorf("alpha without override must default false; got %v", got)
	}
}

func TestReconcile_BetaDefaultsOn(t *testing.T) {
	fg := mkFG("beta-default-on", policyv1alpha1.FeatureGateStageBeta, nil)
	r := newReconciler(t, fg)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	cm := &corev1.ConfigMap{}
	_ = r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm)
	got := map[string]bool{}
	_ = json.Unmarshal([]byte(cm.Data[FeatureGateConfigMapKey]), &got)
	if !got["beta-default-on"] {
		t.Errorf("beta without override must default true; got %v", got)
	}
}

func TestReconcile_StatusEffectivePersists(t *testing.T) {
	fg := mkFG("cosign-installplan-verify", policyv1alpha1.FeatureGateStageAlpha, ptrBool(true))
	r := newReconciler(t, fg)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &policyv1alpha1.FeatureGate{}
	if err := r.Get(context.Background(),
		types.NamespacedName{Name: fg.Name}, got); err != nil {
		t.Fatalf("get fg: %v", err)
	}
	if !got.Status.Effective {
		t.Errorf("expected status.effective=true")
	}
	if got.Status.ObservedGeneration != fg.Generation {
		t.Errorf("expected observedGeneration=%d, got %d",
			fg.Generation, got.Status.ObservedGeneration)
	}
	if got.Status.LastTransitionTime == nil {
		t.Errorf("expected lastTransitionTime to be set")
	}
}

func TestReconcile_DeletedGateDropsFromProjection(t *testing.T) {
	a := mkFG("a", policyv1alpha1.FeatureGateStageBeta, nil)
	b := mkFG("b", policyv1alpha1.FeatureGateStageAlpha, ptrBool(true))
	r := newReconciler(t, a, b)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "a"}}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Delete b and reconcile again.
	if err := r.Delete(context.Background(), b); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "b"}}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	cm := &corev1.ConfigMap{}
	_ = r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm)
	got := map[string]bool{}
	_ = json.Unmarshal([]byte(cm.Data[FeatureGateConfigMapKey]), &got)
	if _, present := got["b"]; present {
		t.Errorf("deleted gate must not appear in projection; got %v", got)
	}
	if !got["a"] {
		t.Errorf("surviving gate a missing from projection; got %v", got)
	}
}

func TestReconcile_NoOpOnUnchangedProjection(t *testing.T) {
	fg := mkFG("cosign-installplan-verify", policyv1alpha1.FeatureGateStageAlpha, ptrBool(true))
	r := newReconciler(t, fg)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("first: %v", err)
	}
	cm1 := &corev1.ConfigMap{}
	if err := r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm1); err != nil {
		t.Fatalf("get cm1: %v", err)
	}
	rv1 := cm1.ResourceVersion
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: fg.Name}}); err != nil {
		t.Fatalf("second: %v", err)
	}
	cm2 := &corev1.ConfigMap{}
	_ = r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm2)
	if cm2.ResourceVersion != rv1 {
		t.Errorf("CM was rewritten despite no change: rv1=%s rv2=%s",
			rv1, cm2.ResourceVersion)
	}
}

func TestReconcile_NoCMOnEmptyList(t *testing.T) {
	r := newReconciler(t)
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "any"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	cm := &corev1.ConfigMap{}
	err := r.Get(context.Background(),
		types.NamespacedName{Name: FeatureGateConfigMapName, Namespace: "test-ns"}, cm)
	if err != nil && !errors.IsNotFound(err) {
		// We allow either: empty CM or no CM at all.
		t.Fatalf("unexpected error: %v", err)
	}
}
