// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultHMACSecretName is the Secret holding the shared HMAC key for
	// CrossTenantAgreement SA-token approvals. CI pipelines that emit
	// approval annotations sign with the same secret; the operator
	// verifies on each reconcile. Rotation: bump the Secret value and CI;
	// in-flight approvals from the old key fail until reissued.
	DefaultHMACSecretName = "keese-cra-hmac"
	// DefaultHMACSecretKey is the Secret key holding the raw shared secret.
	DefaultHMACSecretKey = "secret"
	// DefaultHMACSecretNamespace is where the Secret lives.
	DefaultHMACSecretNamespace = "keese-system"
)

// HMACSATokenVerifier is the production SATokenHmacVerifier. It reads the
// shared HMAC secret on each Verify call (so rotation propagates without an
// operator restart) and compares HMAC-SHA256(audience) against the supplied
// hex-encoded signature using constant-time hmac.Equal.
//
// Signature scheme:
//
//	tag = HMAC-SHA256(secret, audience)
//	signature_annotation = hex(tag)
//
// CI pipelines that emit a CrossTenantAgreement approval annotation must
// produce the signature with this exact scheme; any change is a breaking
// rotation that must be coordinated with downstream.
type HMACSATokenVerifier struct {
	Client          client.Client
	SecretName      string // default DefaultHMACSecretName
	SecretKey       string // default DefaultHMACSecretKey
	SecretNamespace string // default DefaultHMACSecretNamespace
}

// NewHMACSATokenVerifier returns a verifier wired against the keese-system
// HMAC secret. Override the Secret fields directly on the returned struct if
// a non-default location is needed (test fixtures, multi-cluster overlays).
func NewHMACSATokenVerifier(c client.Client) *HMACSATokenVerifier {
	return &HMACSATokenVerifier{
		Client:          c,
		SecretName:      DefaultHMACSecretName,
		SecretKey:       DefaultHMACSecretKey,
		SecretNamespace: DefaultHMACSecretNamespace,
	}
}

// Verify implements SATokenHmacVerifier.
func (h *HMACSATokenVerifier) Verify(ctx context.Context, token, audience string) error {
	if token == "" {
		return errSignatureVerificationFailed
	}
	got, err := hex.DecodeString(token)
	if err != nil {
		return fmt.Errorf("hmac: decode signature: %w", err)
	}

	var sec corev1.Secret
	key := types.NamespacedName{
		Name:      h.secretName(),
		Namespace: h.secretNamespace(),
	}
	if err := h.Client.Get(ctx, key, &sec); err != nil {
		return fmt.Errorf("hmac: read shared secret %s: %w", key, err)
	}
	shared, ok := sec.Data[h.secretKey()]
	if !ok || len(shared) == 0 {
		return errors.New("hmac: shared secret key missing or empty")
	}

	mac := hmac.New(sha256.New, shared)
	if _, err := mac.Write([]byte(audience)); err != nil {
		return fmt.Errorf("hmac: write audience: %w", err)
	}
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, got) {
		return errSignatureVerificationFailed
	}
	return nil
}

func (h *HMACSATokenVerifier) secretName() string {
	if h.SecretName != "" {
		return h.SecretName
	}
	return DefaultHMACSecretName
}

func (h *HMACSATokenVerifier) secretKey() string {
	if h.SecretKey != "" {
		return h.SecretKey
	}
	return DefaultHMACSecretKey
}

func (h *HMACSATokenVerifier) secretNamespace() string {
	if h.SecretNamespace != "" {
		return h.SecretNamespace
	}
	return DefaultHMACSecretNamespace
}

var _ SATokenHmacVerifier = (*HMACSATokenVerifier)(nil)
