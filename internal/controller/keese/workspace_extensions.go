// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// extension↔workspace linkage
//
// A Workspace does NOT carry an explicit list of RuntimeExtensions. The binding
// is indirect, through the shared cluster-scoped AgentRuntime:
//
//   - Workspace.spec.runtimeRef.Name  → AgentRuntime
//   - RuntimeExtension.spec.runtimeRef.Name → AgentRuntime
//
// A RuntimeExtension is "enabled in" a Workspace when both reference the same
// AgentRuntime. This is the `extension:E#enabled_in@workspace:W` tuple the
// runtime spec (docs/specs/keese.ai-v1alpha1-runtime.md, ReBAC lifecycle)
// promises is "written on workspace create / deleted on workspace teardown".
//
// The tuple is written via the RuntimeRebacWriter (WriteExtensionEnabledIn), the
// same writer the RuntimeExtension controller uses to count boundWorkspaces. The
// RuntimeExtension controller's CountEnabledIn then reflects the bound count into
// RuntimeExtension.status.boundWorkspaces on its next reconcile.
//
// Extensions are matched within the Workspace's own namespace: RuntimeExtension
// is namespaced, and a Workspace enables only extensions co-located in its
// namespace (cross-namespace extension sharing is out of scope here — the
// AgentRuntime is cluster-scoped but the extension binding is namespace-local,
// matching how the RuntimeExtension owner tuple resolves its tenant from the
// extension's own namespace).

// enabledExtensionsFor lists the RuntimeExtensions in the Workspace's namespace
// whose spec.runtimeRef matches the Workspace's spec.runtimeRef — i.e. the
// extensions enabled in this Workspace. Returns a stable (API-ordered) slice.
func (r *WorkspaceReconciler) enabledExtensionsFor(
	ctx context.Context,
	ws *keesev1alpha1.Workspace,
) ([]keesev1alpha1.RuntimeExtension, error) {
	runtimeName := ws.Spec.RuntimeRef.Name
	if runtimeName == "" {
		return nil, nil
	}

	var list keesev1alpha1.RuntimeExtensionList
	if err := r.List(ctx, &list, client.InNamespace(ws.Namespace)); err != nil {
		return nil, fmt.Errorf("list runtimeextensions: %w", err)
	}

	matched := make([]keesev1alpha1.RuntimeExtension, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Spec.RuntimeRef.Name == runtimeName {
			matched = append(matched, list.Items[i])
		}
	}
	return matched, nil
}

// syncEnabledInTuples writes one extension:E#enabled_in@workspace:W tuple per
// RuntimeExtension enabled in this Workspace. Idempotent — re-running writes the
// same tuple set with no error. Emits ExtensionTupleWritten when ≥1 tuple is
// written. A nil RuntimeRebac writer is treated as a no-op so a partially-wired
// reconciler never panics.
func (r *WorkspaceReconciler) syncEnabledInTuples(
	ctx context.Context,
	ws *keesev1alpha1.Workspace,
) error {
	if r.RuntimeRebac == nil {
		return nil
	}
	log := logf.FromContext(ctx)

	exts, err := r.enabledExtensionsFor(ctx, ws)
	if err != nil {
		return err
	}

	written := 0
	for i := range exts {
		ext := &exts[i]
		if err := r.RuntimeRebac.WriteExtensionEnabledIn(ctx, ext.Name, ws.Name); err != nil {
			return fmt.Errorf("write enabled_in tuple for extension %q: %w", ext.Name, err)
		}
		written++
	}

	if written > 0 {
		log.V(1).Info("wrote enabled_in tuples", "count", written, "workspace", ws.Name)
		r.Recorder.Eventf(ws, corev1.EventTypeNormal, ReasonExtensionTupleWritten,
			"%d extension enabled_in tuple(s) written for workspace %q", written, ws.Name)
	}
	return nil
}

// deleteEnabledInTuples removes the extension:E#enabled_in@workspace:W tuples
// written for this Workspace, on finalizer teardown. Idempotent — re-running
// after the tuples are gone is a no-op. Emits ExtensionTupleDeleted when ≥1
// tuple is removed.
func (r *WorkspaceReconciler) deleteEnabledInTuples(
	ctx context.Context,
	ws *keesev1alpha1.Workspace,
) error {
	if r.RuntimeRebac == nil {
		return nil
	}
	log := logf.FromContext(ctx)

	exts, err := r.enabledExtensionsFor(ctx, ws)
	if err != nil {
		return err
	}

	deleted := 0
	for i := range exts {
		ext := &exts[i]
		if err := r.RuntimeRebac.DeleteExtensionEnabledIn(ctx, ext.Name, ws.Name); err != nil {
			return fmt.Errorf("delete enabled_in tuple for extension %q: %w", ext.Name, err)
		}
		deleted++
	}

	if deleted > 0 {
		log.V(1).Info("deleted enabled_in tuples", "count", deleted, "workspace", ws.Name)
		r.Recorder.Eventf(ws, corev1.EventTypeNormal, ReasonExtensionTupleDeleted,
			"%d extension enabled_in tuple(s) deleted for workspace %q", deleted, ws.Name)
	}
	return nil
}
