// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// cert-manager GVK constants for Certificate objects.
// GroupVersion: cert-manager.io/v1 (cert-manager ≥ v1.0.0).
const (
	certManagerGroup   = "cert-manager.io"
	certManagerVersion = "v1"
)

// certificateGVK is the GVK for cert-manager Certificate objects.
var certificateGVK = schema.GroupVersionKind{
	Group:   certManagerGroup,
	Version: certManagerVersion,
	Kind:    "Certificate",
}

// CertManagerReader checks whether a cert-manager Certificate object exists.
// Used by the Transport reconciler to validate certificateRef fields without
// blocking on cert-manager controller-readiness.
//
// Production: ClientCertManagerReader (this file) — unstructured Get.
// Tests: FakeCertManagerReader defined below.
type CertManagerReader interface {
	// CertificateExists returns true if a cert-manager Certificate with the given
	// name exists in the given namespace.
	CertificateExists(ctx context.Context, namespace, name string) (bool, error)
}

// CertificateProjector creates or updates a cert-manager Certificate for a Transport.
// The controller calls this when Transport.spec.nats.tls or spec.a2a.mutualTLS requests
// a controller-managed Certificate (identified by the cert-managed annotation).
//
// Production: ClientCertificateProjector (this file) — SSA via unstructured.
// Tests: FakeCertificateProjector defined below.
type CertificateProjector interface {
	// ProjectCertificate SSA-applies a cert-manager Certificate object.
	// Parameters:
	//   namespace    — namespace of the Certificate (typically the Transport's namespace).
	//   name         — Certificate name (convention: keese-transport-<transport-name>-tls).
	//   dnsNames     — SANs to encode in the certificate (e.g. NATS server hostnames).
	//   issuerName   — ClusterIssuer name (tenant-default: keese-<tenant-namespace>).
	ProjectCertificate(ctx context.Context, namespace, name string, dnsNames []string, issuerName string) error
}

// --- Production implementations ---

// ClientCertManagerReader is the production CertManagerReader backed by a
// controller-runtime client. It performs an unstructured Get against
// cert-manager.io/v1 Certificate to avoid importing the cert-manager API
// package (which carries incompatible k8s version dependencies).
type ClientCertManagerReader struct {
	client client.Client
}

// NewClientCertManagerReader constructs a ClientCertManagerReader.
func NewClientCertManagerReader(c client.Client) *ClientCertManagerReader {
	return &ClientCertManagerReader{client: c}
}

// Verify interface at compile time.
var _ CertManagerReader = (*ClientCertManagerReader)(nil)

// CertificateExists returns true when a cert-manager Certificate with the given
// name/namespace is present in the API server. Returns (false, nil) when absent,
// (false, err) on unexpected API errors.
func (r *ClientCertManagerReader) CertificateExists(ctx context.Context, namespace, name string) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(certificateGVK)

	err := r.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get Certificate %s/%s: %w", namespace, name, err)
	}
	return true, nil
}

// ClientCertificateProjector is the production CertificateProjector backed by a
// controller-runtime client. It SSA-applies cert-manager.io/v1.Certificate objects
// using fieldOwner keese-transport-controller (rule 04.7).
//
// Secret material: the Certificate's secret lives in the operator namespace;
// the resulting K8s Secret is later mounted via projected volume to workspace
// session pods (rule 05.7 — secrets as projected files, never env vars).
type ClientCertificateProjector struct {
	client client.Client
}

// NewClientCertificateProjector constructs a ClientCertificateProjector.
func NewClientCertificateProjector(c client.Client) *ClientCertificateProjector {
	return &ClientCertificateProjector{client: c}
}

// Verify interface at compile time.
var _ CertificateProjector = (*ClientCertificateProjector)(nil)

// ProjectCertificate SSA-applies a cert-manager Certificate referencing the tenant's
// ClusterIssuer. The secretName follows the convention keese-transport-<name>-tls so
// the resulting K8s Secret can be mounted predictably by WorkspaceSession pods.
//
// The Certificate uses a 90-day duration with 30-day renewal window; these values
// are aligned with the session pod terminationGracePeriodSeconds in config/manager/.
func (p *ClientCertificateProjector) ProjectCertificate(
	ctx context.Context,
	namespace, name string,
	dnsNames []string,
	issuerName string,
) error {
	secretName := "keese-transport-" + name + "-tls"

	// Convert dnsNames []string to []interface{} for unstructured embedding.
	dnsNamesIface := make([]interface{}, len(dnsNames))
	for i, d := range dnsNames {
		dnsNamesIface[i] = d
	}

	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": certManagerGroup + "/" + certManagerVersion,
			"kind":       "Certificate",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "keese-transport-controller",
				},
			},
			"spec": map[string]interface{}{
				"secretName": secretName,
				"dnsNames":   dnsNamesIface,
				// 90-day duration; renewal begins 30 days before expiry.
				"duration":    "2160h",
				"renewBefore": "720h",
				"issuerRef": map[string]interface{}{
					"name":  issuerName,
					"kind":  "ClusterIssuer",
					"group": certManagerGroup,
				},
				"privateKey": map[string]interface{}{
					"algorithm": "ECDSA",
					"size":      int64(256),
				},
			},
		},
	}
	// creationTimestamp must be zero-valued for SSA apply manifests.
	desired.SetCreationTimestamp(metav1.Time{})

	if err := p.client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(transportFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA Certificate %s/%s: %w", namespace, name, err)
	}
	return nil
}

// --- Test fakes ---

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

// FakeCertificateProjector is a CertificateProjector used in tests.
type FakeCertificateProjector struct {
	// Projected records all ProjectCertificate calls as "namespace/name".
	Projected []string
	// FailNext causes the next call to return an error.
	FailNext bool
}

func NewFakeCertificateProjector() *FakeCertificateProjector {
	return &FakeCertificateProjector{}
}

func (f *FakeCertificateProjector) ProjectCertificate(_ context.Context, namespace, name string, _ []string, _ string) error {
	if f.FailNext {
		f.FailNext = false
		return certManagerError("fake certificate projection failure")
	}
	f.Projected = append(f.Projected, namespace+"/"+name)
	return nil
}

var _ CertificateProjector = &FakeCertificateProjector{}

type certManagerError string

func (e certManagerError) Error() string { return string(e) }
