// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHMACSATokenVerifier(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	const secret = "super-secret-shared-key"
	const audience = "keese-egress-tenant-a"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(audience))
	validSig := hex.EncodeToString(mac.Sum(nil))

	makeVerifier := func(secretData map[string][]byte) *HMACSATokenVerifier {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: DefaultHMACSecretName, Namespace: DefaultHMACSecretNamespace},
			Data:       secretData,
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build()
		return NewHMACSATokenVerifier(c)
	}

	t.Run("valid signature accepted", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{DefaultHMACSecretKey: []byte(secret)})
		if err := v.Verify(context.Background(), validSig, audience); err != nil {
			t.Fatalf("Verify(valid): %v", err)
		}
	})

	t.Run("wrong signature rejected", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{DefaultHMACSecretKey: []byte(secret)})
		err := v.Verify(context.Background(), hex.EncodeToString(make([]byte, sha256.Size)), audience)
		if !errors.Is(err, errSignatureVerificationFailed) {
			t.Fatalf("expected errSignatureVerificationFailed, got %v", err)
		}
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{DefaultHMACSecretKey: []byte(secret)})
		err := v.Verify(context.Background(), validSig, "keese-egress-tenant-b")
		if !errors.Is(err, errSignatureVerificationFailed) {
			t.Fatalf("expected errSignatureVerificationFailed, got %v", err)
		}
	})

	t.Run("missing secret rejected", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{})
		err := v.Verify(context.Background(), validSig, audience)
		if err == nil {
			t.Fatalf("expected error when secret key absent")
		}
	})

	t.Run("invalid hex rejected", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{DefaultHMACSecretKey: []byte(secret)})
		err := v.Verify(context.Background(), "not-hex", audience)
		if err == nil {
			t.Fatalf("expected hex decode error")
		}
	})

	t.Run("empty token rejected", func(t *testing.T) {
		t.Parallel()
		v := makeVerifier(map[string][]byte{DefaultHMACSecretKey: []byte(secret)})
		err := v.Verify(context.Background(), "", audience)
		if !errors.Is(err, errSignatureVerificationFailed) {
			t.Fatalf("expected errSignatureVerificationFailed, got %v", err)
		}
	})
}
