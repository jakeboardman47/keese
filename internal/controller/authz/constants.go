// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "time"

// managedLabel and managedLabelValue are the predicate label required on all
// managed resources processed by controllers in this package.
const (
	managedLabel      = "keese.ai/managed"
	managedLabelValue = "true"

	// requeueAfterBackoff is the requeue interval on transient errors (shared across controllers).
	requeueAfterBackoff = 5 * time.Second
)
