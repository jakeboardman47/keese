// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NACK Stream CRD coordinates. The keese-transport controller already projects
// jetstream.nats.io/v1beta2.Stream CRs; we delete them by name here.
var nackStreamGVK = schema.GroupVersionKind{
	Group:   "jetstream.nats.io",
	Version: "v1beta2",
	Kind:    "Stream",
}

// NACKStreamDeleter is the production NatsStreamDeleter. It deletes the NACK
// Stream CR whose name matches the given stream name; the NATS operator
// (nack) then tears down the underlying JetStream stream.
//
// Uses unstructured Delete so this controller does not need to take a direct
// dependency on the nack Go module just to delete a CR by name.
type NACKStreamDeleter struct {
	Client client.Client
	// Namespace is where the Stream CRs live. Typically the operator
	// namespace (keese-system) since the transport controller projects
	// them there.
	Namespace string
}

// NewNACKStreamDeleter constructs a deleter targeting the given namespace.
func NewNACKStreamDeleter(c client.Client, namespace string) *NACKStreamDeleter {
	if namespace == "" {
		namespace = "keese-system"
	}
	return &NACKStreamDeleter{Client: c, Namespace: namespace}
}

// DeleteStream implements NatsStreamDeleter. Idempotent — returns nil on
// NotFound (already deleted) or NoKindMatch (NACK CRD not installed).
func (d *NACKStreamDeleter) DeleteStream(ctx context.Context, streamName string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(nackStreamGVK)
	obj.SetNamespace(d.Namespace)
	obj.SetName(streamName)

	if err := d.Client.Delete(ctx, obj); err != nil {
		switch {
		case errors.IsNotFound(err), meta.IsNoMatchError(err):
			return nil
		default:
			return fmt.Errorf("delete NACK Stream %s/%s: %w", d.Namespace, streamName, err)
		}
	}
	return nil
}

var _ NatsStreamDeleter = (*NACKStreamDeleter)(nil)
