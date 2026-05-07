// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// RuntimeRebacWriter is the interface the RuntimeExtension controller uses to manage
// OpenFGA tuples. Callers never reference OpenFGA directly — this boundary
// lets tests inject a fake writer (see runtime_rebac_fake_test.go) without a live
// OpenFGA instance. The real implementation (RuntimeOpenFGARebacWriter) is wired
// at startup via cmd/main.go when OPENFGA_API_URL is set.
type RuntimeRebacWriter interface {
	// WriteExtensionOwner writes the tuple:
	//   extension:<extensionName>#owner@tenant:<tenantName>
	WriteExtensionOwner(ctx context.Context, extensionName, tenantName string) error

	// DeleteExtensionOwner deletes the owner tuple.
	DeleteExtensionOwner(ctx context.Context, extensionName, tenantName string) error

	// WriteExtensionEnabledIn writes the tuple:
	//   extension:<extensionName>#enabled_in@workspace:<workspaceName>
	WriteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error

	// DeleteExtensionEnabledIn deletes a single enabled_in tuple.
	DeleteExtensionEnabledIn(ctx context.Context, extensionName, workspaceName string) error

	// DeleteAllExtensionTuples removes all tuples (owner + all enabled_in) for extensionName.
	// Used by the finalizer cleanup path.
	DeleteAllExtensionTuples(ctx context.Context, extensionName string) (int, error)

	// CountEnabledIn returns the number of active enabled_in tuples for extensionName.
	CountEnabledIn(ctx context.Context, extensionName string) (int, error)
}

// RuntimeNoopRebacWriter is a silent no-op RuntimeRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type RuntimeNoopRebacWriter struct{}

func (RuntimeNoopRebacWriter) WriteExtensionOwner(_ context.Context, _, _ string) error   { return nil }
func (RuntimeNoopRebacWriter) DeleteExtensionOwner(_ context.Context, _, _ string) error  { return nil }
func (RuntimeNoopRebacWriter) WriteExtensionEnabledIn(_ context.Context, _, _ string) error { return nil }
func (RuntimeNoopRebacWriter) DeleteExtensionEnabledIn(_ context.Context, _, _ string) error { return nil }
func (RuntimeNoopRebacWriter) DeleteAllExtensionTuples(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (RuntimeNoopRebacWriter) CountEnabledIn(_ context.Context, _ string) (int, error) { return 0, nil }

var _ RuntimeRebacWriter = RuntimeNoopRebacWriter{}
