// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

// Package authz — GuardrailBinding test fake declarations.
//
// The envtest environment, manager lifecycle, and reconciler wiring for this
// package all live in suite_test.go, which is the canonical Ginkgo bootstrap
// for the authz package integration tests. This file only declares the fake
// dependency variables referenced by guardrailbinding_controller_test.go.
package authz

var (
	fakeRebac   *FakeRebacWriter
	fakeKyverno *FakeKyvernoProjector
	fakeEnvoy   *FakeEnvoyProjector
)
