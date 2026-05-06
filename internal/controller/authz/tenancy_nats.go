// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// NatsStreamDeleter deletes the NATS JetStream stream associated with a
// CrossTenantAgreement on finalizer cleanup. The real implementation calls
// the nats.go NACK client; the fake is used in tests.
//
// TODO(spec-followup): implement real NATS JetStream stream deletion via
// github.com/nats-io/nack once NATS is in go.mod. Stream name pattern:
// keese-cta-<cra-uid>.
type NatsStreamDeleter interface {
	// DeleteStream removes the JetStream stream with the given name.
	// Returns nil if the stream does not exist (idempotent).
	DeleteStream(ctx context.Context, streamName string) error
}

// FakeNatsStreamDeleter is a no-op NatsStreamDeleter used in tests.
// It records deletion calls for assertion.
type FakeNatsStreamDeleter struct {
	Deleted []string
	// FailNext causes the next DeleteStream call to return an error.
	FailNext bool
}

func (f *FakeNatsStreamDeleter) DeleteStream(_ context.Context, streamName string) error {
	if f.FailNext {
		f.FailNext = false
		return natsDeleteError{stream: streamName}
	}
	f.Deleted = append(f.Deleted, streamName)
	return nil
}

var _ NatsStreamDeleter = &FakeNatsStreamDeleter{}

type natsDeleteError struct{ stream string }

func (e natsDeleteError) Error() string {
	return "nats: failed to delete stream " + e.stream + " (fake)"
}

// natsStreamName returns the canonical stream name for a CrossTenantAgreement.
func natsStreamName(craUID string) string {
	return "keese-cta-" + craUID
}
