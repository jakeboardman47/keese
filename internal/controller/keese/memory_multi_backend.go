// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// MultiBackendProvisioner dispatches BackendProvisioner calls to the correct
// per-provider implementation based on provider.Type. It is the production
// BackendProvisioner wired into MemoryReconciler.SetupWithManager.
//
// Each per-provider backend is idempotent: repeated Provision calls converge
// in ≤3 reconciles (rule 04.16).
type MultiBackendProvisioner struct {
	sqlite  *SQLiteBackend
	redis   *RedisBackend
	qdrant  *QdrantBackend
	pgvector *PGVectorBackend
	neo4j   *Neo4jBackend
	mem0    *Mem0Backend
	zep     *ZepBackend
}

// NewMultiBackendProvisioner constructs a MultiBackendProvisioner with all 7
// per-provider backends wired to the same controller client.
func NewMultiBackendProvisioner(c client.Client) *MultiBackendProvisioner {
	return &MultiBackendProvisioner{
		sqlite:   NewSQLiteBackend(c),
		redis:    NewRedisBackend(c),
		qdrant:   NewQdrantBackend(c),
		pgvector: NewPGVectorBackend(c),
		neo4j:    NewNeo4jBackend(c),
		mem0:     NewMem0Backend(c),
		zep:      NewZepBackend(c),
	}
}

// Provision implements BackendProvisioner.
func (m *MultiBackendProvisioner) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	switch provider.Type {
	case keesev1alpha1.ProviderSQLite:
		return m.sqlite.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderRedis:
		return m.redis.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderQdrant:
		return m.qdrant.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderPGVector:
		return m.pgvector.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderNeo4j:
		return m.neo4j.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderMem0:
		return m.mem0.Provision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderZep:
		return m.zep.Provision(ctx, provider, name, namespace)
	default:
		return false, fmt.Errorf("unknown memory provider type %q", provider.Type)
	}
}

// Deprovision implements BackendProvisioner.
func (m *MultiBackendProvisioner) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	switch provider.Type {
	case keesev1alpha1.ProviderSQLite:
		return m.sqlite.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderRedis:
		return m.redis.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderQdrant:
		return m.qdrant.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderPGVector:
		return m.pgvector.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderNeo4j:
		return m.neo4j.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderMem0:
		return m.mem0.Deprovision(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderZep:
		return m.zep.Deprovision(ctx, provider, name, namespace)
	default:
		return fmt.Errorf("unknown memory provider type %q for deprovision", provider.Type)
	}
}

// Healthy implements BackendProvisioner.
func (m *MultiBackendProvisioner) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	switch provider.Type {
	case keesev1alpha1.ProviderSQLite:
		return m.sqlite.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderRedis:
		return m.redis.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderQdrant:
		return m.qdrant.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderPGVector:
		return m.pgvector.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderNeo4j:
		return m.neo4j.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderMem0:
		return m.mem0.Healthy(ctx, provider, name, namespace)
	case keesev1alpha1.ProviderZep:
		return m.zep.Healthy(ctx, provider, name, namespace)
	default:
		return false, fmt.Errorf("unknown memory provider type %q for health check", provider.Type)
	}
}
