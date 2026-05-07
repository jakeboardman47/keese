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

// NACK JetStream CRD group / version. The CRDs are registered by the nack Helm chart.
// GroupVersion: jetstream.nats.io/v1beta2 (nack ≥ v0.6).
const (
	nackGroup   = "jetstream.nats.io"
	nackVersion = "v1beta2"
)

// nackStreamGVK is the GVK for the NACK Stream CRD.
var nackStreamGVK = schema.GroupVersionKind{
	Group:   nackGroup,
	Version: nackVersion,
	Kind:    "Stream",
}

// ClientNatsStreamer is the production NatsStreamer backed by a controller-runtime client.
// It projects NACK jetstream.nats.io/v1beta2.Stream CRDs via Server-Side Apply (SSA)
// with fieldOwner keese-transport-controller (rule 04.7).
//
// The operator namespace hosts the Stream CRs; the stream name is taken directly
// from Transport.spec.nats.streamName.
type ClientNatsStreamer struct {
	client    client.Client
	namespace string
}

// NewClientNatsStreamer constructs a ClientNatsStreamer targeting the given namespace.
// Typically this is the operator namespace (keese-system) since Stream CRs are cluster-scoped
// resources projected into the operator's home namespace.
func NewClientNatsStreamer(c client.Client, namespace string) *ClientNatsStreamer {
	return &ClientNatsStreamer{client: c, namespace: namespace}
}

// Verify the interface at compile time.
var _ NatsStreamer = (*ClientNatsStreamer)(nil)

// StreamExists checks whether a jetstream.nats.io/v1beta2.Stream with the given name
// exists. Returns (true, &StreamInfo{...}, nil) when found, (false, nil, nil) when absent,
// and (false, nil, err) on API error.
func (s *ClientNatsStreamer) StreamExists(ctx context.Context, streamName string) (bool, *StreamInfo, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(nackStreamGVK)

	err := s.client.Get(ctx, types.NamespacedName{
		Namespace: s.namespace,
		Name:      streamName,
	}, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("get Stream %s/%s: %w", s.namespace, streamName, err)
	}

	// Extract subjects from spec.subjects for the StreamInfo return value.
	subjects, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "subjects")
	return true, &StreamInfo{Name: streamName, Subjects: subjects}, nil
}

// AddStream SSA-projects a NACK Stream CRD with the given configuration.
// The fieldOwner is keese-transport-controller (rule 04.7). ForceOwnership ensures
// the controller can reclaim fields owned by a previous manager (e.g. manual kubectl apply).
func (s *ClientNatsStreamer) AddStream(ctx context.Context, cfg StreamConfig) error {
	desired := s.buildStreamObject(cfg)
	if err := s.client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(transportFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA Stream %s/%s: %w", s.namespace, cfg.Name, err)
	}
	return nil
}

// UpdateStream SSA-updates an existing NACK Stream CRD. Internally this is the same
// SSA Patch operation as AddStream — SSA is idempotent; calling it on an existing
// object updates only the fields owned by this controller.
func (s *ClientNatsStreamer) UpdateStream(ctx context.Context, cfg StreamConfig) error {
	return s.AddStream(ctx, cfg)
}

// DeleteStream removes the NACK Stream CRD. Returns nil if the stream is already absent
// (idempotent per rule 04.6).
func (s *ClientNatsStreamer) DeleteStream(ctx context.Context, streamName string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(nackStreamGVK)
	obj.SetName(streamName)
	obj.SetNamespace(s.namespace)

	if err := s.client.Delete(ctx, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete Stream %s/%s: %w", s.namespace, streamName, err)
	}
	return nil
}

// buildStreamObject constructs the unstructured Stream desired state from cfg.
// Subject naming follows design 03c (workflow messaging plane):
//   keese.tenant.<t>.transport.<name>.*
// When no subjects are configured, the default subject pattern is derived from the
// stream name.
func (s *ClientNatsStreamer) buildStreamObject(cfg StreamConfig) *unstructured.Unstructured {
	subjects := cfg.Subjects
	if len(subjects) == 0 {
		// Default subject pattern follows design 03c naming convention.
		subjects = []string{fmt.Sprintf("keese.transport.%s.>", cfg.Name)}
	}

	storage := cfg.Storage
	if storage == "" {
		storage = "file"
	}
	retention := cfg.Retention
	if retention == "" {
		retention = "limits"
	}
	maxAge := cfg.MaxAge
	if maxAge == "" {
		maxAge = "168h" // 7 days expressed as duration string
	}
	replicas := int64(cfg.Replicas)
	if replicas <= 0 {
		replicas = 3
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": nackGroup + "/" + nackVersion,
			"kind":       "Stream",
			"metadata": map[string]interface{}{
				"name":      cfg.Name,
				"namespace": s.namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "keese-transport-controller",
				},
			},
			"spec": map[string]interface{}{
				"name":           cfg.Name,
				"subjects":       toInterfaceSlice(subjects),
				"retention":      retention,
				"maxAge":         maxAge,
				"storage":        storage,
				"replicas":       replicas,
				"discard":        "old",
				"maxMsgsPerSubject": int64(-1),
			},
		},
	}
	// creationTimestamp must be set for SSA to accept the manifest.
	obj.SetCreationTimestamp(metav1.Time{})
	return obj
}

// toInterfaceSlice converts a []string to []interface{} for unstructured embedding.
func toInterfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
