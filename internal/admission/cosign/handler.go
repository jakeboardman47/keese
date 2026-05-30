// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/keese-ai/keese/internal/featuregate"
)

// Break-glass annotation + label, anchored from rule 05.13. The
// webhook honors them only when both are present: the annotation
// names the explicit override on the InstallPlan; the namespace
// label proves the cluster operator opted-in to break-glass mode for
// the namespace.
const (
	BreakGlassAnnotation = "keese.ai/unsafe-allow-unsigned"
	BreakGlassNSLabel    = "keese.ai/break-glass"
)

// Reasons emitted in admission.Response.Result. Used by the audit
// trail and by tests; do not free-form here (rule 04.11 — finite
// const table).
const (
	ReasonAllowed         = "Allowed"
	ReasonAllowedNoGates  = "AllowedNoGatedImages"
	ReasonAllowedBreakGla = "AllowedBreakGlass"
	ReasonAllowedGateOff  = "AllowedFeatureGateOff"
	ReasonAllowedDryRun   = "BundleUnsignedAdmittedDryRun"
	ReasonDeniedUnsigned  = "BundleUnsigned"
	ReasonDeniedNoDigest  = "BundleNotDigestPinned"
	ReasonDeniedBadCSV    = "InstallPlanCSVUnreadable"
	ReasonDeniedBadShape  = "InstallPlanMalformed"
)

// GateReader is the minimum surface the handler needs from
// internal/featuregate. Defined here so tests can inject a stub
// without spinning up a full Gates instance.
type GateReader interface {
	Enabled(ctx context.Context, gate featuregate.Gate) bool
}

// Handler is the admission.Handler that gates OLM InstallPlans on
// keese-image cosign verification. Spec: design 14a §4 + rule 05.12.
//
// Gates is optional. nil → cosign verification runs unconditionally
// in fail-closed mode (the original TD-P1-04 behavior).
type Handler struct {
	Client   ctrlclient.Client
	Verifier *Verifier
	Decoder  admission.Decoder
	Log      logr.Logger
	Gates    GateReader
}

// Handle implements admission.Handler.
//
// Flow:
//
//  1. Decode req → InstallPlan (unstructured).
//  2. Resolve CSV names → images (relatedImages + deployment specs).
//  3. For each image gated by the verifier prefix list, run cosign
//     verify keyless — fail-closed.
//  4. Honor break-glass when both annotation + namespace label are
//     present.
//
// All exit paths emit a structured log line at info level.
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := h.Log.WithValues(
		"namespace", req.Namespace,
		"name", req.Name,
		"uid", req.UID,
		"operation", req.Operation,
	)

	if !h.gateEnabled(ctx, featuregate.CosignInstallPlanVerify, true) {
		log.Info("cosign verify gate is off — passing through",
			"reason", ReasonAllowedGateOff,
			"gate", featuregate.CosignInstallPlanVerify)
		return stampedAllow(ReasonAllowedGateOff,
			"feature-gate cosign-installplan-verify=false")
	}

	ip := &unstructured.Unstructured{}
	ip.SetGroupVersionKind(InstallPlanGVK)
	if err := h.Decoder.Decode(req, ip); err != nil {
		log.Error(err, "decode InstallPlan")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if h.breakGlass(ctx, ip) {
		log.Info("break-glass override accepted", "reason", ReasonAllowedBreakGla)
		return stampedAllow(ReasonAllowedBreakGla,
			"namespace break-glass + InstallPlan annotation set")
	}

	ref, err := parseInstallPlan(ip)
	if err != nil {
		log.Error(err, "parse InstallPlan")
		return stampedDeny(http.StatusBadRequest, ReasonDeniedBadShape, err.Error())
	}

	csvs, err := fetchCSVs(ctx, h.Client, ref)
	if err != nil {
		log.Error(err, "fetch CSVs", "csvs", ref.CSVNames)
		return stampedDeny(http.StatusUnprocessableEntity, ReasonDeniedBadCSV,
			err.Error())
	}

	gatedImages := []string{}
	for _, csv := range csvs {
		images, err := imagesFromCSV(csv)
		if err != nil {
			log.Error(err, "extract images", "csv", csv.GetName())
			return stampedDeny(http.StatusUnprocessableEntity, ReasonDeniedBadCSV,
				fmt.Sprintf("extract images from CSV %s: %v", csv.GetName(), err))
		}
		for _, img := range images {
			if h.Verifier.Gates(img) {
				gatedImages = append(gatedImages, img)
			}
		}
	}

	if len(gatedImages) == 0 {
		log.Info("no gated images — passing through",
			"reason", ReasonAllowedNoGates, "csvs", ref.CSVNames)
		return stampedAllow(ReasonAllowedNoGates,
			"no images matched the keese registry allowlist")
	}

	failClosed := h.gateEnabled(ctx, featuregate.CosignInstallPlanFailClosed, true)
	for _, img := range gatedImages {
		if err := h.Verifier.Verify(ctx, img); err != nil {
			reason := ReasonDeniedUnsigned
			if errors.Is(err, ErrNotDigestPinned) {
				reason = ReasonDeniedNoDigest
			}
			if !failClosed {
				log.Info("cosign verify failed but failClosed gate is off — admitting with warning",
					"image", img,
					"reason", ReasonAllowedDryRun,
					"underlying_reason", reason,
					"error", err.Error())
				return stampedAllowWarn(ReasonAllowedDryRun,
					fmt.Sprintf("image %s would be denied (%s) but failClosed=false: %v",
						img, reason, err))
			}
			log.Info("cosign verify denied",
				"image", img,
				"reason", reason,
				"error", err.Error())
			return stampedDeny(http.StatusForbidden, reason,
				fmt.Sprintf("image %s rejected: %v", img, err))
		}
	}

	log.Info("cosign verify allowed",
		"reason", ReasonAllowed,
		"images", gatedImages)
	return stampedAllow(ReasonAllowed,
		fmt.Sprintf("verified %d keese image(s)", len(gatedImages)))
}

// gateEnabled returns the effective value of a featuregate, using
// fallback when h.Gates is nil. Centralized so the handler stays
// testable without a real featuregate.Gates instance.
func (h *Handler) gateEnabled(ctx context.Context, g featuregate.Gate, fallback bool) bool {
	if h.Gates == nil {
		return fallback
	}
	return h.Gates.Enabled(ctx, g)
}

// breakGlass returns true iff BOTH the InstallPlan annotation and
// the namespace label are set. Either alone is rejected per rule
// 05.13.
func (h *Handler) breakGlass(ctx context.Context, ip *unstructured.Unstructured) bool {
	if ip.GetAnnotations()[BreakGlassAnnotation] != "true" {
		return false
	}
	ns := &corev1.Namespace{}
	if err := h.Client.Get(ctx,
		types.NamespacedName{Name: ip.GetNamespace()}, ns); err != nil {
		return false
	}
	return ns.Labels[BreakGlassNSLabel] == "true"
}

func stampedAllow(reason, msg string) admission.Response {
	r := admission.Allowed(msg)
	r.Result = &metav1.Status{
		Status:  metav1.StatusSuccess,
		Reason:  metav1.StatusReason(reason),
		Message: msg,
	}
	return r
}

// stampedAllowWarn returns Allowed but attaches a Warning header so
// `kubectl apply` surfaces the dry-run admit. Used when failClosed is
// off and a verification failure is being downgraded.
func stampedAllowWarn(reason, msg string) admission.Response {
	r := stampedAllow(reason, msg)
	r.Warnings = append(r.Warnings, msg)
	return r
}

func stampedDeny(code int32, reason, msg string) admission.Response {
	r := admission.Errored(code, errors.New(msg))
	r.Allowed = false
	r.Result = &metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    code,
		Reason:  metav1.StatusReason(reason),
		Message: msg,
	}
	return r
}
