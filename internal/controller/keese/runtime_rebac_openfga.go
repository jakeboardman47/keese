// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// RuntimeOpenFGARebacWriter implements RuntimeRebacWriter against the central rebac.Client.
type RuntimeOpenFGARebacWriter struct {
	Client *rebac.Client
}

func extObj(extensionName string) string { return "extension:" + extensionName }

func (w *RuntimeOpenFGARebacWriter) WriteExtensionOwner(ctx context.Context, extensionName, tenantName string) error {
	return w.Client.Write(ctx, extObj(extensionName), "owner", "tenant:"+tenantName)
}

func (w *RuntimeOpenFGARebacWriter) DeleteExtensionOwner(ctx context.Context, extensionName, tenantName string) error {
	return w.Client.Delete(ctx, extObj(extensionName), "owner", "tenant:"+tenantName)
}

func (w *RuntimeOpenFGARebacWriter) WriteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error {
	return w.Client.Write(ctx, extObj(extensionName), "enabled_in", "workspace:"+workspaceName)
}

func (w *RuntimeOpenFGARebacWriter) DeleteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error {
	return w.Client.Delete(ctx, extObj(extensionName), "enabled_in", "workspace:"+workspaceName)
}

func (w *RuntimeOpenFGARebacWriter) DeleteAllExtensionTuples(ctx context.Context, extensionName string) (int, error) {
	tuples, err := w.Client.Read(ctx, extObj(extensionName), "", "")
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, t := range tuples {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (w *RuntimeOpenFGARebacWriter) CountEnabledIn(ctx context.Context, extensionName string) (int, error) {
	tuples, err := w.Client.Read(ctx, extObj(extensionName), "enabled_in", "")
	if err != nil {
		return 0, err
	}
	return len(tuples), nil
}

var _ RuntimeRebacWriter = (*RuntimeOpenFGARebacWriter)(nil)
