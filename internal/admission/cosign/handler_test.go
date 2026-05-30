// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/keese-ai/keese/internal/featuregate"
)

// stubGates is a deterministic GateReader for tests. Missing entries
// behave as if the gate is off.
type stubGates map[featuregate.Gate]bool

func (s stubGates) Enabled(_ context.Context, g featuregate.Gate) bool {
	return s[g]
}

// installPlanRaw builds an InstallPlan AdmissionRequest body with
// the supplied csv names + namespace + annotations.
func installPlanRaw(t *testing.T, ns string, csvs []string, ann map[string]string) []byte {
	t.Helper()
	ip := &unstructured.Unstructured{}
	ip.SetGroupVersionKind(InstallPlanGVK)
	ip.SetNamespace(ns)
	ip.SetName("install-test")
	if ann != nil {
		ip.SetAnnotations(ann)
	}
	csvAny := make([]any, 0, len(csvs))
	for _, c := range csvs {
		csvAny = append(csvAny, c)
	}
	ip.Object["spec"] = map[string]any{
		"clusterServiceVersionNames": csvAny,
		"approval":                   "Manual",
	}
	b, err := json.Marshal(ip)
	if err != nil {
		t.Fatalf("marshal InstallPlan: %v", err)
	}
	return b
}

func mkRequest(raw []byte, ns string) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       types.UID("test-uid"),
			Name:      "install-test",
			Namespace: ns,
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Kind: metav1.GroupVersionKind{
				Group:   InstallPlanGVK.Group,
				Version: InstallPlanGVK.Version,
				Kind:    InstallPlanGVK.Kind,
			},
		},
	}
}

// fakeClientWithCSVs returns a fake controller-runtime client preloaded
// with InstallPlan-namespace + the supplied CSV objects.
func fakeClientWithCSVs(
	ns string, nsLabels map[string]string, csvObjs ...*unstructured.Unstructured,
) client.Client {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	// Register the OLM CSV gvk under the unstructured umbrella so the
	// fake client can satisfy gets via Unstructured kind discovery.
	scheme.AddKnownTypeWithName(CSVGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{
			Group: CSVGVK.Group, Version: CSVGVK.Version, Kind: CSVGVK.Kind + "List",
		},
		&unstructured.UnstructuredList{},
	)

	objects := []client.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns,
				Labels: nsLabels,
			},
		},
	}
	for _, c := range csvObjs {
		objects = append(objects, c)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func mkSignedCSV(name, ns string, images []string) *unstructured.Unstructured {
	csv := mkCSV(images, nil)
	csv.SetName(name)
	csv.SetNamespace(ns)
	return csv
}

func mkHandler(t *testing.T, c client.Client, cosignPath string) *Handler {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{
		CosignBinary:            cosignPath,
		AllowedRegistryPrefixes: []string{"ghcr.io/keese-ai/"},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	scheme.AddKnownTypeWithName(InstallPlanGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(CSVGVK, &unstructured.Unstructured{})
	return &Handler{
		Client:   c,
		Verifier: v,
		Decoder:  admission.NewDecoder(scheme),
		Log:      zap.New(zap.UseDevMode(true)),
	}
}

func TestHandle_AllowsSignedKeeseImage(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese@sha256:" + strings.Repeat("a", 64)})
	c := fakeClientWithCSVs(ns, nil, csv)
	h := mkHandler(t, c, fakeCosign(t, 0, "Verification ok"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if !resp.Allowed {
		t.Fatalf("expected Allowed, got %+v", resp)
	}
	if resp.Result == nil || resp.Result.Reason != metav1.StatusReason(ReasonAllowed) {
		t.Errorf("expected reason=%s, got %+v", ReasonAllowed, resp.Result)
	}
}

func TestHandle_DeniesUnsignedKeeseImage(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese@sha256:" + strings.Repeat("b", 64)})
	c := fakeClientWithCSVs(ns, nil, csv)
	h := mkHandler(t, c, fakeCosign(t, 1, "no matching signatures"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if resp.Allowed {
		t.Fatalf("expected Denied, got Allowed")
	}
	if resp.Result == nil || resp.Result.Reason != metav1.StatusReason(ReasonDeniedUnsigned) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedUnsigned, resp.Result)
	}
	if resp.Result.Code != http.StatusForbidden {
		t.Errorf("expected code=403, got %d", resp.Result.Code)
	}
}

func TestHandle_DeniesTagOnlyKeeseImage(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese:v0.0.1"})
	c := fakeClientWithCSVs(ns, nil, csv)
	h := mkHandler(t, c, fakeCosign(t, 0, "ok"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if resp.Allowed {
		t.Fatalf("expected Denied, got Allowed")
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonDeniedNoDigest) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedNoDigest, resp.Result.Reason)
	}
}

func TestHandle_AllowsNonKeeseImage(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("other.v1", ns,
		[]string{"quay.io/some/operator@sha256:" + strings.Repeat("c", 64)})
	c := fakeClientWithCSVs(ns, nil, csv)
	// cosign would fail if invoked — passes only because we skip non-gated images.
	h := mkHandler(t, c, fakeCosign(t, 1, "should not run"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"other.v1"}, nil), ns))

	if !resp.Allowed {
		t.Fatalf("expected Allowed, got Denied: %+v", resp.Result)
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonAllowedNoGates) {
		t.Errorf("expected reason=%s, got %+v", ReasonAllowedNoGates, resp.Result.Reason)
	}
}

func TestHandle_AllowsBreakGlassWhenBothSet(t *testing.T) {
	const ns = "break-glass-ns"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese:v0.0.1"}) // tag-only, would normally deny
	c := fakeClientWithCSVs(ns,
		map[string]string{BreakGlassNSLabel: "true"}, csv)
	// Cosign would not be invoked thanks to break-glass; route to a
	// failing fake to assert that.
	h := mkHandler(t, c, fakeCosign(t, 1, "must not run under break-glass"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"},
			map[string]string{BreakGlassAnnotation: "true"}), ns))

	if !resp.Allowed {
		t.Fatalf("expected Allowed under break-glass, got %+v", resp.Result)
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonAllowedBreakGla) {
		t.Errorf("expected reason=%s, got %+v", ReasonAllowedBreakGla, resp.Result.Reason)
	}
}

func TestHandle_RejectsBreakGlassAnnotationAlone(t *testing.T) {
	const ns = "no-label"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese:v0.0.1"})
	c := fakeClientWithCSVs(ns, nil, csv) // no break-glass label
	h := mkHandler(t, c, fakeCosign(t, 0, "ok"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"},
			map[string]string{BreakGlassAnnotation: "true"}), ns))

	if resp.Allowed {
		t.Fatalf("annotation alone must not bypass — namespace lacks break-glass label")
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonDeniedNoDigest) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedNoDigest, resp.Result.Reason)
	}
}

func TestHandle_DeniesMissingCSV(t *testing.T) {
	const ns = "operators"
	c := fakeClientWithCSVs(ns, nil) // no CSV objects
	h := mkHandler(t, c, fakeCosign(t, 0, "ok"))

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if resp.Allowed {
		t.Fatalf("expected Denied, got Allowed")
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonDeniedBadCSV) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedBadCSV, resp.Result.Reason)
	}
}

func TestHandle_DeniesMalformedInstallPlan(t *testing.T) {
	const ns = "operators"
	c := fakeClientWithCSVs(ns, nil)
	h := mkHandler(t, c, fakeCosign(t, 0, "ok"))

	// Build an InstallPlan with no spec.clusterServiceVersionNames.
	ip := &unstructured.Unstructured{}
	ip.SetGroupVersionKind(InstallPlanGVK)
	ip.SetNamespace(ns)
	ip.SetName("install-test")
	ip.Object["spec"] = map[string]any{}
	raw, _ := json.Marshal(ip)

	resp := h.Handle(context.Background(), mkRequest(raw, ns))

	if resp.Allowed {
		t.Fatalf("expected Denied, got Allowed")
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonDeniedBadShape) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedBadShape, resp.Result.Reason)
	}
}

func TestHandle_GateOff_AllowsUnconditionally(t *testing.T) {
	const ns = "operators"
	// CSV with an unsigned, tag-only image — would normally deny on
	// every counts. Gate off should bypass cosign entirely.
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese:v0.0.1"})
	c := fakeClientWithCSVs(ns, nil, csv)
	h := mkHandler(t, c, fakeCosign(t, 1, "must not run when gate is off"))
	h.Gates = stubGates{
		featuregate.CosignInstallPlanVerify: false,
	}

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if !resp.Allowed {
		t.Fatalf("gate-off must Allow; got Denied: %+v", resp.Result)
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonAllowedGateOff) {
		t.Errorf("expected reason=%s, got %+v", ReasonAllowedGateOff, resp.Result.Reason)
	}
}

func TestHandle_FailClosedOff_DowngradesToWarning(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese@sha256:" + strings.Repeat("d", 64)})
	c := fakeClientWithCSVs(ns, nil, csv)
	// Cosign returns non-zero — without failClosed this is admitted with a warning.
	h := mkHandler(t, c, fakeCosign(t, 1, "no matching signatures"))
	h.Gates = stubGates{
		featuregate.CosignInstallPlanVerify:     true,
		featuregate.CosignInstallPlanFailClosed: false,
	}

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if !resp.Allowed {
		t.Fatalf("failClosed=false must Allow on verify failure; got %+v", resp.Result)
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonAllowedDryRun) {
		t.Errorf("expected reason=%s, got %+v", ReasonAllowedDryRun, resp.Result.Reason)
	}
	if len(resp.Warnings) == 0 {
		t.Errorf("expected at least one warning attached to dry-run admit")
	}
}

func TestHandle_FailClosedOn_StillDenies(t *testing.T) {
	const ns = "operators"
	csv := mkSignedCSV("keese.v0.0.1", ns,
		[]string{"ghcr.io/keese-ai/keese@sha256:" + strings.Repeat("e", 64)})
	c := fakeClientWithCSVs(ns, nil, csv)
	h := mkHandler(t, c, fakeCosign(t, 1, "no matching signatures"))
	h.Gates = stubGates{
		featuregate.CosignInstallPlanVerify:     true,
		featuregate.CosignInstallPlanFailClosed: true,
	}

	resp := h.Handle(context.Background(),
		mkRequest(installPlanRaw(t, ns, []string{"keese.v0.0.1"}, nil), ns))

	if resp.Allowed {
		t.Fatalf("failClosed=true must Deny on verify failure; got Allowed")
	}
	if resp.Result.Reason != metav1.StatusReason(ReasonDeniedUnsigned) {
		t.Errorf("expected reason=%s, got %+v", ReasonDeniedUnsigned, resp.Result.Reason)
	}
}
