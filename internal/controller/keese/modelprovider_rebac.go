// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// ModelProviderTuple represents a single OpenFGA relationship tuple written for
// a ModelProvider. Object/Relation/User follow the OpenFGA tuple format
// ("<type>:<id>" / "<relation>" / "<type>:<id>").
type ModelProviderTuple struct {
	Object   string
	Relation string
	User     string
}

// ModelProviderRebacWriter abstracts OpenFGA tuple operations for ModelProvider.
// The real implementation is wired at startup; tests inject a fake. When OpenFGA
// is unconfigured, ModelProviderNoopRebacWriter is the fallback.
type ModelProviderRebacWriter interface {
	// Write writes (upserts) the given tuples.
	Write(ctx context.Context, tuples []ModelProviderTuple) error
	// Delete removes the given tuples. Missing tuples are silently ignored.
	Delete(ctx context.Context, tuples []ModelProviderTuple) error
}

// ModelProviderCredentialTuple returns the tuple recording that a ModelProvider
// binds a credential Secret as its egress credential source. ext_authz consults
// this when selecting which upstream credential the gateway injects
// (+keese:rebac-tuple=modelprovider.credential).
func ModelProviderCredentialTuple(providerID, secretName string) ModelProviderTuple {
	return ModelProviderTuple{
		Object:   "modelprovider:" + providerID,
		Relation: "credential",
		User:     "secret:" + secretName,
	}
}

// ModelProviderNoopRebacWriter is a silent no-op writer used when OpenFGA is not
// configured (dev/local run without OPENFGA_API_URL).
type ModelProviderNoopRebacWriter struct{}

// Write implements ModelProviderRebacWriter.
func (ModelProviderNoopRebacWriter) Write(_ context.Context, _ []ModelProviderTuple) error {
	return nil
}

// Delete implements ModelProviderRebacWriter.
func (ModelProviderNoopRebacWriter) Delete(_ context.Context, _ []ModelProviderTuple) error {
	return nil
}
