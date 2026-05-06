// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"time"
)

// NatsStreamSpec describes the desired JetStream stream configuration.
type NatsStreamSpec struct {
	// Name is the JetStream stream name.
	// Convention: "keese-tenant-<tenant-uid>-wf-<run-uid>"
	Name string

	// Subjects lists the NATS subjects the stream captures.
	// Convention: "keese.tenant.<t>.wf.<r>.>"
	Subjects []string

	// MaxAge is the maximum age of a message before deletion.
	// Maps to spec.timeout on the WorkflowRun.
	MaxAge time.Duration

	// Replicas is the stream replica count (3 for production).
	Replicas int

	// WorkloadOwnerUID is the Kubernetes UID of the owning Argo Workflow.
	// Used by the stream watcher for garbage collection.
	WorkloadOwnerUID string
}

// NatsStreamProvisioner creates JetStream streams for WorkflowRuns.
// The real implementation uses the nats.go NATS client; tests use FakeNatsStreamProvisioner.
type NatsStreamProvisioner interface {
	// Provision creates (or idempotently updates) the JetStream stream
	// described by spec. Returns the actual stream name on success.
	Provision(ctx context.Context, spec NatsStreamSpec) (string, error)
}

// NatsStreamDeleter deletes JetStream streams during WorkflowRun cleanup.
type NatsStreamDeleter interface {
	// Delete removes the named JetStream stream. Returns nil if the stream
	// does not exist (idempotent).
	Delete(ctx context.Context, streamName string) error
}

// FakeNatsStreamProvisioner is a test-only NatsStreamProvisioner.
type FakeNatsStreamProvisioner struct {
	// Provisioned accumulates Provision calls.
	Provisioned []NatsStreamSpec
	// ReturnName overrides the stream name returned. Defaults to spec.Name.
	ReturnName string
	// Err is returned on all Provision calls when non-nil.
	Err error
}

// Provision records the call and returns the stream name.
func (f *FakeNatsStreamProvisioner) Provision(_ context.Context, spec NatsStreamSpec) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	f.Provisioned = append(f.Provisioned, spec)
	if f.ReturnName != "" {
		return f.ReturnName, nil
	}
	return spec.Name, nil
}

// FakeNatsStreamDeleter is a test-only NatsStreamDeleter.
type FakeNatsStreamDeleter struct {
	// Deleted accumulates Delete calls.
	Deleted []string
	// Err is returned on all Delete calls when non-nil.
	Err error
}

// Delete records the stream name and returns nil (or Err).
func (f *FakeNatsStreamDeleter) Delete(_ context.Context, streamName string) error {
	if f.Err != nil {
		return f.Err
	}
	f.Deleted = append(f.Deleted, streamName)
	return nil
}

// Verify interfaces at compile time.
var _ NatsStreamProvisioner = (*FakeNatsStreamProvisioner)(nil)
var _ NatsStreamDeleter = (*FakeNatsStreamDeleter)(nil)
