// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimev1alpha1 "github.com/keese-ai/keese/api/runtime/v1alpha1"
)

// resolveAgentRuntime fetches the cluster-scoped AgentRuntime referenced by the
// Workspace. Returns an error if the runtime is missing, unset, or not goose
// (the only provider implemented at v1alpha1 demo).
func resolveAgentRuntime(
	ctx context.Context,
	c client.Reader,
	name string,
) (*runtimev1alpha1.AgentRuntime, error) {
	if name == "" {
		return nil, fmt.Errorf("workspace.spec.runtimeRef.name is empty")
	}
	var ar runtimev1alpha1.AgentRuntime
	// AgentRuntime is cluster-scoped — namespace omitted.
	if err := c.Get(ctx, client.ObjectKey{Name: name}, &ar); err != nil {
		return nil, fmt.Errorf("get AgentRuntime %q: %w", name, err)
	}
	if ar.Spec.Implementation.Goose == nil {
		return nil, fmt.Errorf("AgentRuntime %q has no goose implementation; demo only supports goose", name)
	}
	if ar.Spec.Implementation.Goose.Image == "" {
		return nil, fmt.Errorf("AgentRuntime %q goose image is empty", name)
	}
	return &ar, nil
}
