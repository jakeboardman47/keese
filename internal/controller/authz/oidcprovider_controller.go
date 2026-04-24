// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// placeholderRe matches any substring of the form {token} or <token> where token
// starts with a letter or underscore and contains only word-chars and hyphens.
// Examples: {tenant-id}, <okta-domain>, <keycloak-host>, <realm>.
// The regex uses a non-greedy interior so mismatched-bracket strings like
// "https://{broken-" (no closing brace) do NOT match.
var placeholderRe = regexp.MustCompile(`[{<][A-Za-z_][A-Za-z0-9_-]*[>}]`)

const (
	// requeuePlaceholderInterval is how often a bootstrap CR with a placeholder issuer
	// is re-checked. The GenerationChangedPredicate will also trigger earlier if the
	// admin updates the issuer.
	requeuePlaceholderInterval = 1 * time.Hour

	// oidcProviderFinalizer is placed on every OIDCProvider CR that has been
	// reconciled, ensuring the cache-flush signal is sent to gateway pods before
	// Kubernetes removes the object. Format per rule 04.10.
	oidcProviderFinalizer = "finalizers.oidcprovider.operator.keese.ai/cache-flush"

	// oidcFieldOwner is the SSA field manager identifier (rule 04.7).
	oidcFieldOwner = "keese-oidcprovider-controller"

	// bootstrapFieldOwner is the SSA field manager used by the install Job for
	// bootstrap CRs (e.g. kubernetes-default, google, github-actions).
	bootstrapFieldOwner = "keese-oidcprovider-bootstrap"

	// bootstrapLabel and its value identify operator-managed bootstrap CRs.
	bootstrapLabel      = "keese.ai/bootstrap"
	bootstrapLabelValue = "true"

	// managedLabel and its value filter CRs processed by this reconciler.
	managedLabel      = "keese.ai/managed"
	managedLabelValue = "true"

	// requeueJWKSInterval is how often the controller re-probes the JWKS endpoint
	// regardless of spec changes (rule 06 — no time.Sleep; use RequeueAfter).
	requeueJWKSInterval = 5 * time.Minute

	// maxFlushTimeout is the maximum time we wait for the cache-flush signal on
	// deletion before proceeding anyway (rule 04.10).
	maxFlushTimeout = 60 * time.Second

	// conditionReady and conditionJWKSReachable are the canonical condition type strings.
	conditionReady         = "Ready"
	conditionJWKSReachable = "JWKSReachable"
)

// Prometheus metrics (rule — operational readiness).
// All registered at package init time so they survive controller restarts without duplication.
var (
	metricTemplateEvalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_oidc_template_eval_errors_total",
		Help: "Total number of OIDCProvider template evaluation errors.",
	}, []string{"provider", "template", "reason"})

	metricAudienceTemplateEval = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_oidc_audience_template_eval_total",
		Help: "Total number of OIDCProvider audience template evaluations.",
	}, []string{"provider", "template", "result"})

	metricTokenRotation = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "keese_oidc_token_rotation_seconds",
		Help:    "Observed token rotation durations for OIDCProvider audience templates.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider", "template"})

	metricJWKSFetchFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_gateway_jwks_fetch_failures_total",
		Help: "Total number of JWKS endpoint fetch failures per provider.",
	}, []string{"provider"})

	metricCacheInvalidations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "keese_oidc_cache_invalidations_total",
		Help: "Total number of cache invalidation signals sent per provider and trigger.",
	}, []string{"provider", "trigger"})
)

// OIDCProviderReconciler reconciles an OIDCProvider object.
// SSA fieldOwner: keese-oidcprovider-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.oidcprovider.operator.keese.ai/cache-flush
//     Sends gRPC cache-flush to all gateway pods (max 60s drain) before deletion.
//
// Design: docs/designs/04b-projected-sa-identity.md + docs/designs/04b-ii-oidc-trust.md
// Spec: docs/specs/authz.operator.keese.ai-v1alpha1.md
type OIDCProviderReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	JwksFetcher  JwksFetcher
	CacheFlusher CacheFlusher
	HTTPClient   *http.Client // optional — used for OIDC discovery
}

// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=authz.operator.keese.ai,resources=oidcproviders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the main reconciliation loop for OIDCProvider.
// Idiom: fetch → deepcopy for status patch → handle deletion → validate templates →
// probe JWKS → update status → requeue after JWKS interval.
func (r *OIDCProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var provider authzv1alpha1.OIDCProvider
	if err := r.Get(ctx, req.NamespacedName, &provider); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := provider.DeepCopy()

	// Handle deletion first (rule 04.10 — always check DeletionTimestamp first).
	if !provider.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &provider, orig)
	}

	// Ensure finalizer is present before any external resource is touched.
	if !controllerutil.ContainsFinalizer(&provider, oidcProviderFinalizer) {
		controllerutil.AddFinalizer(&provider, oidcProviderFinalizer)
		if err := r.Patch(ctx, &provider, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Re-fetch after patch so orig is accurate for subsequent status patch.
		if err := r.Get(ctx, req.NamespacedName, &provider); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = provider.DeepCopy()
	}

	// --- Placeholder-issuer guard (bootstrap CRs) ---
	// Bootstrap CRs may ship with template placeholders (e.g. {tenant-id}, <okta-domain>)
	// that admins must override before the provider is usable. Skip the full reconcile
	// path so we do not hammer unreachable placeholder URLs on every JWKS probe cycle.
	if isBootstrapCR(&provider) && detectPlaceholderIssuer(provider.Spec.Issuer) {
		provider.Status.Phase = authzv1alpha1.OIDCProviderPhaseDegraded
		provider.Status.ObservedGeneration = provider.Generation
		setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonBootstrapPlaceholderIssuer,
			Message:            fmt.Sprintf("issuer %q contains a placeholder; admin must override before first reconcile", provider.Spec.Issuer),
			ObservedGeneration: provider.Generation,
		})
		r.Recorder.Eventf(&provider, corev1.EventTypeWarning, ReasonBootstrapPlaceholderIssuer,
			"issuer %q contains a placeholder; admin must override before first reconcile", provider.Spec.Issuer)
		if err := r.patchStatus(ctx, &provider, orig); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeuePlaceholderInterval}, nil
	}

	// --- Template validation ---
	if err := r.validateTemplates(ctx, &provider, orig); err != nil {
		// validateTemplates patches status and emits events; return error to requeue.
		return ctrl.Result{}, err
	}

	// --- JWKS reachability probe ---
	r.probeJWKS(ctx, &provider)

	// --- Update status fields ---
	provider.Status.ObservedGeneration = provider.Generation
	now := metav1.Now()
	provider.Status.LastReconcileTime = now

	log.Info("reconcile complete",
		"phase", provider.Status.Phase,
		"observedGeneration", provider.Status.ObservedGeneration,
	)

	if err := r.patchStatus(ctx, &provider, orig); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue periodically to re-probe JWKS even without spec changes.
	return ctrl.Result{RequeueAfter: requeueJWKSInterval}, nil
}

// validateTemplates parses subjectTemplate and all audienceTemplates using the
// restricted Sprig allow-list. On parse error it sets Degraded + Ready=False.
// On success it sets Active + lastTemplateValidationTime.
func (r *OIDCProviderReconciler) validateTemplates(
	ctx context.Context,
	provider *authzv1alpha1.OIDCProvider,
	orig *authzv1alpha1.OIDCProvider,
) error {
	// Build the input slice for ValidateTemplates.
	audienceInputs := make([]struct{ Name, Template string }, len(provider.Spec.AudienceTemplates))
	for i, at := range provider.Spec.AudienceTemplates {
		audienceInputs[i] = struct{ Name, Template string }{Name: at.Name, Template: at.Template}
	}

	if err := ValidateTemplates(provider.Spec.SubjectTemplate, audienceInputs); err != nil {
		metricTemplateEvalErrors.WithLabelValues(provider.Name, "subjectTemplate", "parse_error").Inc()
		provider.Status.Phase = authzv1alpha1.OIDCProviderPhaseDegraded
		setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "TemplateInvalid",
			Message:            err.Error(),
			ObservedGeneration: provider.Generation,
		})
		r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonTemplateInvalid,
			"Template parse failed: %v", err)
		return r.patchStatus(ctx, provider, orig)
	}

	// Templates valid — advance to Active.
	provider.Status.Phase = authzv1alpha1.OIDCProviderPhaseActive
	now := metav1.Now()
	provider.Status.LastTemplateValidationTime = now
	setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "TemplateValid",
		Message:            "All templates parsed successfully",
		ObservedGeneration: provider.Generation,
	})

	for _, at := range provider.Spec.AudienceTemplates {
		metricAudienceTemplateEval.WithLabelValues(provider.Name, at.Name, "success").Inc()
	}

	r.Recorder.Eventf(provider, corev1.EventTypeNormal, ReasonTemplateValidationSucceeded,
		"All templates validated for %s", provider.Name)
	return nil
}

// probeJWKS checks reachability of the JWKS endpoint and updates JWKSReachable condition.
// Phase is NOT changed on JWKS failure — template validity and JWKS are independent concerns.
func (r *OIDCProviderReconciler) probeJWKS(ctx context.Context, provider *authzv1alpha1.OIDCProvider) {
	jwksURI := provider.Spec.JWKSUri
	if jwksURI == "" {
		// Derive from issuer via OpenID Connect discovery.
		// TODO(spec-followup): caching the derived JWKS URI in status would avoid
		// a discovery round-trip on every reconcile; defer until status field is specified.
		var err error
		jwksURI, err = DeriveJWKSURI(ctx, r.HTTPClient, provider.Spec.Issuer)
		if err != nil {
			metricJWKSFetchFailures.WithLabelValues(provider.Name).Inc()
			setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
				Type:               conditionJWKSReachable,
				Status:             metav1.ConditionFalse,
				Reason:             "DiscoveryFailed",
				Message:            fmt.Sprintf("OIDC discovery failed: %v", err),
				ObservedGeneration: provider.Generation,
			})
			r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonJWKSUnreachable,
				"OIDC discovery failed for issuer %s: %v", provider.Spec.Issuer, err)
			return
		}
	}

	if err := r.JwksFetcher.Fetch(ctx, jwksURI); err != nil {
		metricJWKSFetchFailures.WithLabelValues(provider.Name).Inc()
		setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
			Type:               conditionJWKSReachable,
			Status:             metav1.ConditionFalse,
			Reason:             "FetchFailed",
			Message:            fmt.Sprintf("JWKS fetch failed: %v", err),
			ObservedGeneration: provider.Generation,
		})
		r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonJWKSUnreachable,
			"JWKS endpoint %s unreachable: %v", jwksURI, err)
		return
	}

	setOIDCCondition(&provider.Status.Conditions, metav1.Condition{
		Type:               conditionJWKSReachable,
		Status:             metav1.ConditionTrue,
		Reason:             "Reachable",
		Message:            fmt.Sprintf("JWKS endpoint %s is reachable", jwksURI),
		ObservedGeneration: provider.Generation,
	})
	r.Recorder.Eventf(provider, corev1.EventTypeNormal, ReasonJWKSReachable,
		"JWKS endpoint %s is reachable", jwksURI)
}

// cleanup runs when DeletionTimestamp is set. It sends a cache-flush signal to gateway
// pods (max 60s), then removes the finalizer. On flush timeout, deletion proceeds
// and a warning event is emitted.
func (r *OIDCProviderReconciler) cleanup(
	ctx context.Context,
	provider *authzv1alpha1.OIDCProvider,
	orig *authzv1alpha1.OIDCProvider,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(provider, oidcProviderFinalizer) {
		return ctrl.Result{}, nil
	}

	flushCtx, flushCancel := context.WithTimeout(ctx, maxFlushTimeout)
	defer flushCancel()

	if err := r.CacheFlusher.Flush(flushCtx, provider.Name); err != nil {
		if flushCtx.Err() != nil {
			// Timeout — proceed with deletion rather than block forever.
			r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonCacheFlushTimeout,
				"Cache flush timed out after %s; proceeding with deletion: %v", maxFlushTimeout, err)
			r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonJWKSUnreachable,
				"Gateway cache may be stale for provider %s after timeout", provider.Name)
		} else {
			// Transient error — retry.
			r.Recorder.Eventf(provider, corev1.EventTypeWarning, ReasonCacheFlushTimeout,
				"Cache flush failed: %v; will retry", err)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, r.patchStatus(ctx, provider, orig)
		}
	} else {
		metricCacheInvalidations.WithLabelValues(provider.Name, "deletion").Inc()
		r.Recorder.Eventf(provider, corev1.EventTypeNormal, ReasonCacheFlushComplete,
			"Cache flush completed for provider %s", provider.Name)
	}

	controllerutil.RemoveFinalizer(provider, oidcProviderFinalizer)
	return ctrl.Result{}, r.Patch(ctx, provider, client.MergeFrom(orig))
}

// patchStatus patches only the status subresource, preserving spec.
func (r *OIDCProviderReconciler) patchStatus(
	ctx context.Context,
	provider *authzv1alpha1.OIDCProvider,
	orig *authzv1alpha1.OIDCProvider,
) error {
	return r.Status().Patch(ctx, provider, client.MergeFrom(orig))
}

// isBootstrapCR returns true if the CR carries the bootstrap label.
// Bootstrap CRs have fields managed by keese-oidcprovider-bootstrap field manager;
// this controller only SSAs its own fields and does not clobber bootstrap-owned fields.
func isBootstrapCR(provider *authzv1alpha1.OIDCProvider) bool {
	return provider.Labels[bootstrapLabel] == bootstrapLabelValue
}

// SetupWithManager sets up the controller with the Manager.
func (r *OIDCProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.JwksFetcher == nil {
		r.JwksFetcher = &HTTPJwksFetcher{}
	}
	if r.CacheFlusher == nil {
		r.CacheFlusher = &FakeCacheFlusher{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("oidcprovider-controller")
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&authzv1alpha1.OIDCProvider{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("authz-oidcprovider").
		Complete(r)
}

// detectPlaceholderIssuer returns true when the issuer URL contains a template
// placeholder token that an admin must replace before the provider is usable.
// Recognized forms:
//   - {token}  — curly-brace style (e.g. Azure Entra {tenant-id})
//   - <token>  — angle-bracket style (e.g. Okta <okta-domain>, Keycloak <realm>)
//
// The token must start with a letter or underscore and contain only word chars
// and hyphens — this prevents false positives on valid IPv6 addresses, HTML
// entities, and template/regex syntax that would otherwise contain < or {.
// Mismatched brackets (e.g. "https://{broken-" with no closing brace) do NOT
// match because the regex requires a closing delimiter.
func detectPlaceholderIssuer(issuer string) bool {
	return placeholderRe.MatchString(issuer)
}

// setOIDCCondition upserts a condition into the slice (by Type).
// Preserves LastTransitionTime when Status is unchanged.
func setOIDCCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	now := metav1.Now()
	for i, existing := range *conditions {
		if existing.Type == c.Type {
			if existing.Status != c.Status {
				c.LastTransitionTime = now
			} else {
				c.LastTransitionTime = existing.LastTransitionTime
			}
			(*conditions)[i] = c
			return
		}
	}
	c.LastTransitionTime = now
	*conditions = append(*conditions, c)
}
