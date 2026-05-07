// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// TransportFakeRebacWriter is a no-op TransportRebacWriter used in tests.
// It records Sync and Delete calls for assertion.
type TransportFakeRebacWriter struct {
	Synced  []TransportRebacTuple
	Deleted []TransportRebacTuple
	// FailNext causes the next Sync call to return an error.
	FailNext bool
}

func (f *TransportFakeRebacWriter) Sync(_ context.Context, tuples []TransportRebacTuple) error {
	if f.FailNext {
		f.FailNext = false
		return transportRebacError("fake rebac sync failure")
	}
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *TransportFakeRebacWriter) Delete(_ context.Context, tuples []TransportRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ TransportRebacWriter = &TransportFakeRebacWriter{}

type transportRebacError string

func (e transportRebacError) Error() string { return string(e) }
