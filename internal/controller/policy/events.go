// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

// Event reason constants for the TokenBudget controller.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// ReasonBudgetActive is emitted when the budget phase transitions to Ready.
	ReasonBudgetActive = "BudgetActive"

	// ReasonBudgetExceeded is emitted when consumed tokens cross a limit threshold.
	ReasonBudgetExceeded = "BudgetExceeded"

	// ReasonBudgetReset is emitted when a window boundary passes and counters are reset.
	ReasonBudgetReset = "BudgetReset"

	// ReasonMetricFetchFailed is emitted when the Prometheus query returns an error.
	ReasonMetricFetchFailed = "MetricFetchFailed"

	// ReasonBudgetSignalWriteFailed is emitted when writing to the NATS KV budget signal fails.
	ReasonBudgetSignalWriteFailed = "BudgetSignalWriteFailed"

	// ReasonBudgetEnforcementUnavailable is emitted when both the controller and NATS are down.
	ReasonBudgetEnforcementUnavailable = "BudgetEnforcementUnavailable"

	// ReasonTooManyBudgets is emitted when the cluster-wide TokenBudget CR ceiling is reached.
	ReasonTooManyBudgets = "TooManyBudgets"
)
