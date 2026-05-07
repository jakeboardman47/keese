// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// StreamInfo contains the information about a JetStream stream returned by a lookup.
type StreamInfo struct {
	// Name is the stream name.
	Name string
	// Subjects is the slice of subject patterns.
	Subjects []string
}

// StreamConfig holds the desired JetStream stream configuration.
type StreamConfig struct {
	// Name is the stream name.
	Name string
	// Subjects is the subject pattern list.
	Subjects []string
	// Retention policy: "limits", "interest", or "workqueue".
	Retention string
	// MaxAge is the maximum message age (e.g. "7d").
	MaxAge string
	// Storage: "file" or "memory".
	Storage string
	// Replicas is the replication factor.
	Replicas int32
}

// NatsStreamer manages JetStream stream lifecycle.
//
// Production: ClientNatsStreamer (transport_nats_nack.go) — SSA-projects
// jetstream.nats.io/v1beta2.Stream CRDs via the controller-runtime client.
// Tests: FakeNatsStreamer defined below.
type NatsStreamer interface {
	// StreamExists returns true and stream info if the named stream exists, false otherwise.
	StreamExists(ctx context.Context, streamName string) (bool, *StreamInfo, error)

	// AddStream creates a new JetStream stream with the given configuration.
	AddStream(ctx context.Context, cfg StreamConfig) error

	// UpdateStream updates an existing JetStream stream's configuration.
	UpdateStream(ctx context.Context, cfg StreamConfig) error

	// DeleteStream deletes the named JetStream stream.
	// Returns nil if the stream does not exist (idempotent).
	DeleteStream(ctx context.Context, streamName string) error
}

// FakeNatsStreamer is a NatsStreamer used in tests. It records operations for assertion
// and allows injecting failures.
type FakeNatsStreamer struct {
	// Streams holds the set of existing stream names.
	Streams map[string]*StreamInfo
	// Added records names of streams created via AddStream.
	Added []string
	// Updated records names of streams updated via UpdateStream.
	Updated []string
	// Deleted records names of streams deleted via DeleteStream.
	Deleted []string
	// FailOnAdd causes the next AddStream call to return an error.
	FailOnAdd bool
	// FailOnDelete causes the next DeleteStream call to return an error.
	FailOnDelete bool
}

func NewFakeNatsStreamer() *FakeNatsStreamer {
	return &FakeNatsStreamer{
		Streams: make(map[string]*StreamInfo),
	}
}

func (f *FakeNatsStreamer) StreamExists(_ context.Context, streamName string) (bool, *StreamInfo, error) {
	info, ok := f.Streams[streamName]
	return ok, info, nil
}

func (f *FakeNatsStreamer) AddStream(_ context.Context, cfg StreamConfig) error {
	if f.FailOnAdd {
		f.FailOnAdd = false
		return natsStreamError{op: "add", name: cfg.Name}
	}
	f.Streams[cfg.Name] = &StreamInfo{Name: cfg.Name, Subjects: cfg.Subjects}
	f.Added = append(f.Added, cfg.Name)
	return nil
}

func (f *FakeNatsStreamer) UpdateStream(_ context.Context, cfg StreamConfig) error {
	f.Streams[cfg.Name] = &StreamInfo{Name: cfg.Name, Subjects: cfg.Subjects}
	f.Updated = append(f.Updated, cfg.Name)
	return nil
}

func (f *FakeNatsStreamer) DeleteStream(_ context.Context, streamName string) error {
	if f.FailOnDelete {
		f.FailOnDelete = false
		return natsStreamError{op: "delete", name: streamName}
	}
	delete(f.Streams, streamName)
	f.Deleted = append(f.Deleted, streamName)
	return nil
}

var _ NatsStreamer = &FakeNatsStreamer{}

type natsStreamError struct {
	op   string
	name string
}

func (e natsStreamError) Error() string {
	return "nats: failed to " + e.op + " stream " + e.name + " (fake)"
}
