// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
)

const (
	tokenBudgetFieldOwner = "keese-tokenbudget-controller"

	// Finalizer ID — format: finalizers.<kind>.keese.ai/<purpose> (rule 04.10).
	tokenBudgetFinalizer = "finalizers.tokenbudget.keese.ai/envoy-ratelimit-cleanup"

	// reconcileInterval is the polling interval between reconcile cycles.
	// 10s matches the design spec (10b §"controller read-interval").
	reconcileInterval = 10 * time.Second

	// clusterCeilingWarnThreshold triggers TooManyBudgets warning events.
	clusterCeilingWarnThreshold = 900
)

// TokenBudgetReconciler reconciles a TokenBudget object.
// SSA fieldOwner: keese-tokenbudget-controller (rule 04.7).
//
// Finalizers managed:
//   - finalizers.tokenbudget.keese.ai/envoy-ratelimit-cleanup
type TokenBudgetReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	PromQuerier   PrometheusQuerier
	NatsSignaler  NatsSignaler
	RateLimitProj RateLimitProjector
}

// +kubebuilder:rbac:groups=policy.keese.ai,resources=tokenbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.keese.ai,resources=tokenbudgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.keese.ai,resources=tokenbudgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop for TokenBudget.
// Idiom: fetch → deepcopy for patch → handle deletion → query Prometheus →
// update consumedCurrent → check exhaustion → project RateLimit → update status.
func (r *TokenBudgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tb policyv1alpha1.TokenBudget
	if err := r.Get(ctx, req.NamespacedName, &tb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := tb.DeepCopy()

	// Handle deletion first (rule 04.10).
	if !tb.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &tb, orig)
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&tb, tokenBudgetFinalizer) {
		controllerutil.AddFinalizer(&tb, tokenBudgetFinalizer)
		if err := r.Patch(ctx, &tb, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, &tb); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tb.DeepCopy()
	}

	// Check cluster-wide cardinality ceiling (warn only — VAP enforces the hard deny at 1200).
	if err := r.checkCardinality(ctx, &tb); err != nil {
		log.Error(err, "cardinality check failed")
	}

	// Initialise window on first reconcile or if windowStart is unset.
	now := time.Now().UTC()
	if tb.Status.WindowStart == "" {
		if err := r.initWindow(ctx, &tb, orig, now); err != nil {
			return ctrl.Result{RequeueAfter: reconcileInterval}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &tb); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tb.DeepCopy()
	}

	// Check whether the window has elapsed; reset if so.
	windowEnd, parseErr := time.Parse(time.RFC3339, tb.Status.WindowEnd)
	if parseErr == nil && now.After(windowEnd) {
		if err := r.resetWindow(ctx, &tb, orig, now); err != nil {
			setCondition(&tb.Status.Conditions, metav1.Condition{
				Type:               "ResetFailed",
				Status:             metav1.ConditionTrue,
				Reason:             "WindowResetFailed",
				Message:            err.Error(),
				ObservedGeneration: tb.Generation,
			})
			tb.Status.Phase = policyv1alpha1.TokenBudgetPhaseResetFailed
			tb.Status.ObservedGeneration = tb.Generation
			_ = r.Status().Patch(ctx, &tb, client.MergeFrom(orig))
			return ctrl.Result{RequeueAfter: reconcileInterval}, nil
		}
		if err := r.Get(ctx, req.NamespacedName, &tb); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = tb.DeepCopy()
	}

	// Determine scope identifiers for Prometheus queries and NATS keys.
	scopeType, scopeID := r.resolveScope(&tb)

	// Query Prometheus for each limit entry and update consumedCurrent.
	metricHealthy := true
	newConsumed := make([]policyv1alpha1.TokenUsageEntry, 0, len(tb.Spec.Limits))

	for _, limit := range tb.Spec.Limits {
		consumed, err := r.queryConsumed(ctx, &tb, scopeType, scopeID, limit.Model)
		if err != nil {
			log.Error(err, "Prometheus query failed", "model", limit.Model)
			r.Recorder.Eventf(&tb, corev1.EventTypeWarning, ReasonMetricFetchFailed,
				"Prometheus query failed for model %s: %v", limit.Model, err)
			metricHealthy = false
			// Keep last known consumed for this model (fail-closed: no false-clear).
			consumed = findConsumed(tb.Status.ConsumedCurrent, limit.Model)
		}
		newConsumed = append(newConsumed, policyv1alpha1.TokenUsageEntry{
			Model:        limit.Model,
			InputTokens:  consumed.InputTokens,
			OutputTokens: consumed.OutputTokens,
			TotalTokens:  consumed.TotalTokens,
		})
	}

	// Update consumed current from Prometheus results (or last-known on failure).
	tb.Status.ConsumedCurrent = newConsumed

	// Set MetricFetchHealthy condition.
	if metricHealthy {
		setCondition(&tb.Status.Conditions, metav1.Condition{
			Type:               "MetricFetchHealthy",
			Status:             metav1.ConditionTrue,
			Reason:             "QuerySucceeded",
			Message:            "Prometheus queries completed successfully",
			ObservedGeneration: tb.Generation,
		})
	} else {
		setCondition(&tb.Status.Conditions, metav1.Condition{
			Type:               "MetricFetchHealthy",
			Status:             metav1.ConditionFalse,
			Reason:             "QueryFailed",
			Message:            "One or more Prometheus queries failed; using last known values",
			ObservedGeneration: tb.Generation,
		})
	}

	// Check exhaustion for each limit entry; write NATS signals and project RateLimit.
	anyExceeded := false
	for i, limit := range tb.Spec.Limits {
		consumed := newConsumed[i]
		exceeded := r.isExceeded(limit, consumed)
		remaining := r.computeRemaining(limit, consumed)
		natsKey := budgetExceededKey(scopeType, scopeID, limit.Model)

		if exceeded {
			anyExceeded = true
			if tb.Spec.ExhaustionMode == policyv1alpha1.ExhaustionModeHard {
				if err := r.NatsSignaler.SetExceeded(ctx, natsKey); err != nil {
					log.Error(err, "NATS KV write failed", "key", natsKey)
					r.Recorder.Eventf(&tb, corev1.EventTypeWarning, ReasonBudgetSignalWriteFailed,
						"NATS KV write failed for key %s: %v", natsKey, err)
				}
			}
			r.Recorder.Eventf(&tb, corev1.EventTypeWarning, ReasonBudgetExceeded,
				"Budget exceeded for model %s (mode=%s)", limit.Model, tb.Spec.ExhaustionMode)
		} else if metricHealthy {
			// Only clear the signal if Prometheus is healthy (no false-clear on failure).
			if err := r.NatsSignaler.ClearExceeded(ctx, natsKey); err != nil {
				log.Error(err, "NATS KV clear failed", "key", natsKey)
			}
		}

		// Project Envoy RateLimitPolicy for this limit entry.
		rlPolicy := RateLimitPolicy{
			Namespace:       tb.Namespace,
			Name:            rateLimitPolicyName(string(tb.UID) + "-" + limit.Model),
			ScopeID:         scopeID,
			Model:           limit.Model,
			RemainingTokens: remaining,
		}
		if err := r.RateLimitProj.Apply(ctx, rlPolicy); err != nil {
			log.Error(err, "RateLimitPolicy apply failed", "model", limit.Model)
		}
	}

	// Update phase.
	switch {
	case anyExceeded && tb.Spec.ExhaustionMode == policyv1alpha1.ExhaustionModeHard:
		tb.Status.Phase = policyv1alpha1.TokenBudgetPhaseExhausted
	case anyExceeded && tb.Spec.ExhaustionMode == policyv1alpha1.ExhaustionModeSoft:
		tb.Status.Phase = policyv1alpha1.TokenBudgetPhaseSoftExhausted
	default:
		if tb.Status.Phase != policyv1alpha1.TokenBudgetPhaseReady {
			r.Recorder.Eventf(&tb, corev1.EventTypeNormal, ReasonBudgetActive,
				"TokenBudget %s/%s is Ready", tb.Namespace, tb.Name)
		}
		tb.Status.Phase = policyv1alpha1.TokenBudgetPhaseReady
	}

	// Update status conditions.
	setCondition(&tb.Status.Conditions, metav1.Condition{
		Type:               "BudgetExceeded",
		Status:             boolCondStatus(anyExceeded),
		Reason:             "BudgetCheck",
		Message:            fmt.Sprintf("budget exceeded: %v", anyExceeded),
		ObservedGeneration: tb.Generation,
	})
	setCondition(&tb.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             boolCondStatus(!anyExceeded || tb.Spec.ExhaustionMode == policyv1alpha1.ExhaustionModeDisabled),
		Reason:             "ReconcileComplete",
		Message:            "TokenBudget reconcile completed",
		ObservedGeneration: tb.Generation,
	})

	now2 := metav1.Now()
	tb.Status.ObservedGeneration = tb.Generation
	tb.Status.LastReconcileTime = &now2

	if err := r.Status().Patch(ctx, &tb, client.MergeFrom(orig)); err != nil {
		if !errors.IsConflict(err) {
			return ctrl.Result{}, fmt.Errorf("patching status: %w", err)
		}
	}

	return ctrl.Result{RequeueAfter: reconcileInterval}, nil
}

// cleanup removes the RateLimitPolicy projection and NATS keys before allowing deletion.
func (r *TokenBudgetReconciler) cleanup(ctx context.Context, tb *policyv1alpha1.TokenBudget, orig *policyv1alpha1.TokenBudget) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tb, tokenBudgetFinalizer) {
		return ctrl.Result{}, nil
	}

	scopeType, scopeID := r.resolveScope(tb)

	for _, limit := range tb.Spec.Limits {
		// Delete projected RateLimitPolicy.
		policyName := rateLimitPolicyName(string(tb.UID) + "-" + limit.Model)
		if err := r.RateLimitProj.Delete(ctx, tb.Namespace, policyName); err != nil {
			log.Error(err, "failed to delete RateLimitPolicy during cleanup", "policy", policyName)
			return ctrl.Result{RequeueAfter: reconcileInterval}, nil
		}
		// Delete NATS KV signal key.
		natsKey := budgetExceededKey(scopeType, scopeID, limit.Model)
		if err := r.NatsSignaler.ClearExceeded(ctx, natsKey); err != nil {
			log.Error(err, "failed to clear NATS KV key during cleanup", "key", natsKey)
			return ctrl.Result{RequeueAfter: reconcileInterval}, nil
		}
	}

	controllerutil.RemoveFinalizer(tb, tokenBudgetFinalizer)
	if err := r.Patch(ctx, tb, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// initWindow sets the initial windowStart and windowEnd based on spec.windowAnchor or now.
func (r *TokenBudgetReconciler) initWindow(ctx context.Context, tb *policyv1alpha1.TokenBudget, orig *policyv1alpha1.TokenBudget, now time.Time) error {
	windowDur, err := parseWindowDuration(tb.Spec.WindowDuration)
	if err != nil {
		return fmt.Errorf("parsing windowDuration %q: %w", tb.Spec.WindowDuration, err)
	}

	var windowStart time.Time
	if tb.Spec.WindowAnchor != "" {
		anchor, err := time.Parse(time.RFC3339, tb.Spec.WindowAnchor)
		if err != nil {
			return fmt.Errorf("parsing windowAnchor: %w", err)
		}
		// Advance anchor until it's the most recent start before now.
		windowStart = anchor
		for windowStart.Add(windowDur).Before(now) {
			windowStart = windowStart.Add(windowDur)
		}
	} else {
		windowStart = now
	}

	tb.Status.WindowStart = windowStart.UTC().Format(time.RFC3339)
	tb.Status.WindowEnd = windowStart.Add(windowDur).UTC().Format(time.RFC3339)

	if err := r.Status().Patch(ctx, tb, client.MergeFrom(orig)); err != nil {
		return fmt.Errorf("patching window init: %w", err)
	}
	return nil
}

// resetWindow advances the window by one duration, archives consumedCurrent → consumedPrevious,
// clears current, deletes NATS KV keys, and emits BudgetReset.
func (r *TokenBudgetReconciler) resetWindow(ctx context.Context, tb *policyv1alpha1.TokenBudget, orig *policyv1alpha1.TokenBudget, now time.Time) error {
	windowDur, err := parseWindowDuration(tb.Spec.WindowDuration)
	if err != nil {
		return fmt.Errorf("parsing windowDuration: %w", err)
	}

	// Advance windowStart until current window covers now.
	windowStart, _ := time.Parse(time.RFC3339, tb.Status.WindowStart)
	for windowStart.Add(windowDur).Before(now) || windowStart.Add(windowDur).Equal(now) {
		windowStart = windowStart.Add(windowDur)
	}

	tb.Status.ConsumedPrevious = tb.Status.ConsumedCurrent
	tb.Status.ConsumedCurrent = nil
	tb.Status.WindowStart = windowStart.UTC().Format(time.RFC3339)
	tb.Status.WindowEnd = windowStart.Add(windowDur).UTC().Format(time.RFC3339)
	tb.Status.Phase = policyv1alpha1.TokenBudgetPhaseReady

	setCondition(&tb.Status.Conditions, metav1.Condition{
		Type:               "BudgetExceeded",
		Status:             metav1.ConditionFalse,
		Reason:             "WindowReset",
		Message:            "Window reset; budget cleared",
		ObservedGeneration: tb.Generation,
	})

	// Clear NATS KV signals for all limit entries.
	scopeType, scopeID := r.resolveScope(tb)
	for _, limit := range tb.Spec.Limits {
		natsKey := budgetExceededKey(scopeType, scopeID, limit.Model)
		_ = r.NatsSignaler.ClearExceeded(ctx, natsKey)
	}

	r.Recorder.Eventf(tb, corev1.EventTypeNormal, ReasonBudgetReset,
		"Budget window reset for %s/%s; previous window archived", tb.Namespace, tb.Name)

	return r.Status().Patch(ctx, tb, client.MergeFrom(orig))
}

// queryConsumed issues the PromQL increase() queries for the given model over
// the current window. Three queries: total, input direction, output direction.
// Returns a TokenUsageEntry with the three fields populated independently so
// limit checks against input-only / output-only / total all reflect reality.
func (r *TokenBudgetReconciler) queryConsumed(ctx context.Context, tb *policyv1alpha1.TokenBudget, scopeType, scopeID, model string) (policyv1alpha1.TokenUsageEntry, error) {
	windowDur := tb.Spec.WindowDuration
	if windowDur == "" {
		windowDur = "720h"
	}

	// Build the tenant/workspace label selector.
	var labelFilter string
	if scopeType == "tenant" {
		labelFilter = fmt.Sprintf(`tenant=%q`, scopeID)
	} else {
		labelFilter = fmt.Sprintf(`workspace=%q`, scopeID)
	}

	modelFilter := ""
	if model != "*" {
		modelFilter = fmt.Sprintf(`,model=%q`, model)
	}

	queryDirection := func(direction string) (int64, error) {
		dirFilter := `,direction=~"input|output"`
		if direction != "" {
			dirFilter = fmt.Sprintf(`,direction=%q`, direction)
		}
		expr := fmt.Sprintf(
			`sum(increase(keese_token_budget_consumed_total{%s%s%s}[%s]))`,
			labelFilter, modelFilter, dirFilter, windowDur,
		)
		result, err := r.PromQuerier.Query(ctx, expr)
		if err != nil {
			return 0, err
		}
		v := int64(result.Value)
		if v < 0 {
			v = 0
		}
		return v, nil
	}

	total, err := queryDirection("")
	if err != nil {
		return policyv1alpha1.TokenUsageEntry{Model: model}, err
	}
	input, err := queryDirection("input")
	if err != nil {
		return policyv1alpha1.TokenUsageEntry{Model: model}, err
	}
	output, err := queryDirection("output")
	if err != nil {
		return policyv1alpha1.TokenUsageEntry{Model: model}, err
	}

	return policyv1alpha1.TokenUsageEntry{
		Model:        model,
		TotalTokens:  total,
		InputTokens:  input,
		OutputTokens: output,
	}, nil
}

// isExceeded returns true if any limit field is set and the corresponding consumed value
// meets or exceeds it.
func (r *TokenBudgetReconciler) isExceeded(limit policyv1alpha1.TokenLimit, consumed policyv1alpha1.TokenUsageEntry) bool {
	if limit.TotalTokens != nil && consumed.TotalTokens >= *limit.TotalTokens {
		return true
	}
	if limit.InputTokens != nil && consumed.InputTokens >= *limit.InputTokens {
		return true
	}
	if limit.OutputTokens != nil && consumed.OutputTokens >= *limit.OutputTokens {
		return true
	}
	return false
}

// computeRemaining returns the minimum remaining tokens across configured limit fields.
// Clamped to 0 — never negative (spec §"clamp to 0 on scale-down").
func (r *TokenBudgetReconciler) computeRemaining(limit policyv1alpha1.TokenLimit, consumed policyv1alpha1.TokenUsageEntry) int64 {
	remaining := int64(1<<62 - 1) // MaxInt62 sentinel.
	if limit.TotalTokens != nil {
		r := *limit.TotalTokens - consumed.TotalTokens
		if r < remaining {
			remaining = r
		}
	}
	if limit.InputTokens != nil {
		r := *limit.InputTokens - consumed.InputTokens
		if r < remaining {
			remaining = r
		}
	}
	if limit.OutputTokens != nil {
		r := *limit.OutputTokens - consumed.OutputTokens
		if r < remaining {
			remaining = r
		}
	}
	if remaining < 0 {
		remaining = 0
	}
	if remaining == int64(1<<62-1) {
		remaining = 0
	}
	return remaining
}

// resolveScope returns (scopeType, scopeID) from the TokenBudget spec.
func (r *TokenBudgetReconciler) resolveScope(tb *policyv1alpha1.TokenBudget) (string, string) {
	if tb.Spec.Scope.Tenant != nil {
		return "tenant", tb.Spec.Scope.Tenant.Name
	}
	if tb.Spec.Scope.Workspace != nil {
		return "workspace", tb.Spec.Scope.Workspace.Name
	}
	// Should not reach here due to CEL VAP validation.
	return "unknown", string(tb.UID)
}

// checkCardinality lists all TokenBudgets and emits a warning event if the threshold is exceeded.
func (r *TokenBudgetReconciler) checkCardinality(ctx context.Context, tb *policyv1alpha1.TokenBudget) error {
	var list policyv1alpha1.TokenBudgetList
	if err := r.List(ctx, &list); err != nil {
		return fmt.Errorf("listing TokenBudgets: %w", err)
	}
	if len(list.Items) > clusterCeilingWarnThreshold {
		r.Recorder.Eventf(tb, corev1.EventTypeWarning, ReasonTooManyBudgets,
			"Cluster has %d TokenBudget CRs; warn threshold is %d", len(list.Items), clusterCeilingWarnThreshold)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TokenBudgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("tokenbudget-controller")
	}
	if r.PromQuerier == nil {
		r.PromQuerier = &FakePrometheusQuerier{}
	}
	if r.NatsSignaler == nil {
		r.NatsSignaler = &FakeNatsSignaler{}
	}
	if r.RateLimitProj == nil {
		r.RateLimitProj = &FakeRateLimitProjector{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.TokenBudget{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("observability-tokenbudget").
		Complete(r)
}

// --- Helpers ---

// setCondition upserts a condition into the slice (by Type).
func setCondition(conditions *[]metav1.Condition, c metav1.Condition) {
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

// findConsumed returns the TokenUsageEntry for the given model from the slice,
// or a zero entry if not found.
func findConsumed(entries []policyv1alpha1.TokenUsageEntry, model string) policyv1alpha1.TokenUsageEntry {
	for _, e := range entries {
		if e.Model == model {
			return e
		}
	}
	return policyv1alpha1.TokenUsageEntry{Model: model}
}

// boolCondStatus converts a bool to a metav1.ConditionStatus.
func boolCondStatus(v bool) metav1.ConditionStatus {
	if v {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// parseWindowDuration parses a window duration string of the form ^[0-9]+(h|d|m)$.
// 'd' is mapped to 24h; 'm' is mapped to time.Minute.
func parseWindowDuration(s string) (time.Duration, error) {
	if s == "" {
		return 720 * time.Hour, nil
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	var n int64
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid window duration %q: %w", s, err)
	}
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * time.Minute, nil
	default:
		return 0, fmt.Errorf("unsupported window duration unit %q in %q", string(unit), s)
	}
}
