// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// CertManagerReader checks whether a cert-manager Certificate object exists.
// Transport references Certificates; it does not create them.
//
// TODO(spec-followup): real implementation uses an unstructured Get against
// cert-manager.io/v1 Certificate GVK once cert-manager is in go.mod.
type CertManagerReader interface {
	// CertificateExists returns true if a cert-manager Certificate with the given
	// name exists in the given namespace.
	CertificateExists(ctx context.Context, namespace, name string) (bool, error)
}

// FakeCertManagerReader is a CertManagerReader used in tests.
type FakeCertManagerReader struct {
	// Existing holds "namespace/name" keys for certificates that exist.
	Existing map[string]bool
	// FailNext causes the next call to return an error.
	FailNext bool
}

func NewFakeCertManagerReader() *FakeCertManagerReader {
	return &FakeCertManagerReader{Existing: make(map[string]bool)}
}

func (f *FakeCertManagerReader) CertificateExists(_ context.Context, namespace, name string) (bool, error) {
	if f.FailNext {
		f.FailNext = false
		return false, certManagerError("fake cert-manager read failure")
	}
	return f.Existing[namespace+"/"+name], nil
}

var _ CertManagerReader = &FakeCertManagerReader{}

type certManagerError string

func (e certManagerError) Error() string { return string(e) }
