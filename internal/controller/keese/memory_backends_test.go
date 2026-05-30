// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Unit tests for the per-backend provisioners.
//
// These tests do NOT exercise Server-Side Apply against a live API server; SSA is
// covered by the envtest integration suite (memory_controller_test.go) via the
// FakeBackendProvisioner. Here we test:
//   - Pure builder functions (no client) for correctness of emitted objects.
//   - External-endpoint / credential-ref modes that are no-ops at the SSA layer.
//   - Health probes that only need Get (no SSA).
//   - MultiBackendProvisioner dispatch routing.
//   - FakeBackendProvisioner round-trip (Provision/Deprovision/Healthy).

package keese

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// newFakeScheme returns a scheme with the types needed for backend tests.
func newFakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("add appsv1 to scheme: %v", err)
	}
	if err := keesev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add keesev1alpha1 to scheme: %v", err)
	}
	return s
}

// ---- FakeBackendProvisioner round-trip (sanity) ----

func TestFakeBackendProvisioner_ProvisionDeprovisionHealthy(t *testing.T) {
	f := NewFakeBackendProvisioner()
	provider := keesev1alpha1.MemoryProvider{Type: keesev1alpha1.ProviderSQLite}

	// First Provision: created=true.
	created, err := f.Provision(context.Background(), provider, "mem", "default")
	if err != nil || !created {
		t.Fatalf("Provision: created=%v err=%v", created, err)
	}
	// Idempotent: second Provision returns created=false.
	created2, err2 := f.Provision(context.Background(), provider, "mem", "default")
	if err2 != nil || created2 {
		t.Fatalf("second Provision: created=%v err=%v", created2, err2)
	}
	// Healthy after Provision.
	ok, err3 := f.Healthy(context.Background(), provider, "mem", "default")
	if err3 != nil || !ok {
		t.Fatalf("Healthy after Provision: ok=%v err=%v", ok, err3)
	}
	// Deprovision; then Healthy = false.
	if err := f.Deprovision(context.Background(), provider, "mem", "default"); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
	ok2, _ := f.Healthy(context.Background(), provider, "mem", "default")
	if ok2 {
		t.Error("Healthy after Deprovision must be false")
	}
}

// ---- RedisBackend ----

func TestRedisBackend_ExternalMode_ProvisionNoOp(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewRedisBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Address: "redis.default.svc:6379"},
	}
	created, err := b.Provision(context.Background(), provider, "mem-redis", "default")
	if err != nil {
		t.Fatalf("unexpected error in external mode: %v", err)
	}
	if created {
		t.Error("external mode must return created=false")
	}
}

func TestRedisBackend_ExternalMode_Healthy(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewRedisBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Address: "redis.default.svc:6379"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-redis", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("external address: should be healthy")
	}
}

func TestRedisBackend_ExternalMode_DeprovisionNoOp(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewRedisBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Address: "redis.default.svc:6379"},
	}
	if err := b.Deprovision(context.Background(), provider, "mem-redis", "default"); err != nil {
		t.Fatalf("deprovision: %v", err)
	}
}

func TestRedisBackend_NilConfig_Error(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewRedisBackend(c)

	provider := keesev1alpha1.MemoryProvider{Type: keesev1alpha1.ProviderRedis, Redis: nil}
	_, err := b.Provision(context.Background(), provider, "m", "ns")
	if err == nil {
		t.Fatal("expected error for nil Redis config")
	}
}

// ---- RedisBackend builder (pure) ----

func TestBuildRedisStatefulSet_SecurityContext(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 2)
	c0 := sts.Spec.Template.Spec.Containers[0]
	if c0.SecurityContext == nil {
		t.Fatal("SecurityContext is nil")
	}
	if c0.SecurityContext.AllowPrivilegeEscalation == nil || *c0.SecurityContext.AllowPrivilegeEscalation {
		t.Error("AllowPrivilegeEscalation must be false")
	}
	if c0.SecurityContext.ReadOnlyRootFilesystem == nil || !*c0.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("ReadOnlyRootFilesystem must be true")
	}
	if *sts.Spec.Replicas != 2 {
		t.Errorf("replicas: want 2 got %d", *sts.Spec.Replicas)
	}
}

func TestBuildRedisStatefulSet_Labels(t *testing.T) {
	sts := buildRedisStatefulSet("my-mem", "ns", "my-mem-redis", 1)
	if sts.Labels["keese.ai/memory"] != "my-mem" {
		t.Errorf("missing keese.ai/memory label: %v", sts.Labels)
	}
	if sts.Labels["app.kubernetes.io/managed-by"] != redisFieldOwner {
		t.Errorf("wrong managed-by: %v", sts.Labels)
	}
}

func TestBuildRedisSvc_Headless(t *testing.T) {
	svc := buildRedisSvc("mem", "ns", "mem-redis")
	if svc.Spec.ClusterIP != "None" {
		t.Errorf("expected headless, got ClusterIP=%q", svc.Spec.ClusterIP)
	}
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != redisPort {
		t.Errorf("wrong port: %v", svc.Spec.Ports)
	}
}

// ---- QdrantBackend ----

func TestQdrantBackend_ExternalMode_ProvisionNoOp(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewQdrantBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:   keesev1alpha1.ProviderQdrant,
		Qdrant: &keesev1alpha1.QdrantConfig{CollectionName: "c", Endpoint: "qdrant:6334"},
	}
	created, err := b.Provision(context.Background(), provider, "mem-q", "default")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if created {
		t.Error("external endpoint: created must be false")
	}
}

func TestQdrantBackend_ExternalMode_Healthy(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewQdrantBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:   keesev1alpha1.ProviderQdrant,
		Qdrant: &keesev1alpha1.QdrantConfig{CollectionName: "c", Endpoint: "qdrant:6334"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-q", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("external endpoint: should be healthy")
	}
}

// ---- QdrantBackend builder (pure) ----

func TestBuildQdrantStatefulSet_Ports(t *testing.T) {
	q := resource.MustParse("2Gi")
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q)
	ports := sts.Spec.Template.Spec.Containers[0].Ports
	found := map[string]bool{}
	for _, p := range ports {
		found[p.Name] = true
	}
	if !found["grpc"] || !found["http"] {
		t.Errorf("expected grpc+http ports, got %v", ports)
	}
}

func TestBuildQdrantSvc_Headless(t *testing.T) {
	svc := buildQdrantSvc("mem", "ns", "mem-qdrant")
	if svc.Spec.ClusterIP != "None" {
		t.Errorf("expected headless, got ClusterIP=%q", svc.Spec.ClusterIP)
	}
}

// ---- PGVectorBackend ----

func TestPGVectorBackend_ExternalMode_ProvisionNoOp(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewPGVectorBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:     keesev1alpha1.ProviderPGVector,
		PGVector: &keesev1alpha1.PGVectorConfig{DSNSecretRef: "pg-dsn"},
	}
	created, err := b.Provision(context.Background(), provider, "mem-pgv", "default")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if created {
		t.Error("external DSN mode: created must be false")
	}
}

func TestPGVectorBackend_ExternalMode_HealthySecretPresent(t *testing.T) {
	s := newFakeScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-dsn", Namespace: "default"},
		Data:       map[string][]byte{"dsn": []byte("postgres://user:pass@host/db")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	b := NewPGVectorBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:     keesev1alpha1.ProviderPGVector,
		PGVector: &keesev1alpha1.PGVectorConfig{DSNSecretRef: "pg-dsn"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-pgv", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("secret present: should be healthy")
	}
}

func TestPGVectorBackend_ExternalMode_HealthySecretMissing(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewPGVectorBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:     keesev1alpha1.ProviderPGVector,
		PGVector: &keesev1alpha1.PGVectorConfig{DSNSecretRef: "missing"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-pgv", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if ok {
		t.Error("missing secret: should be unhealthy")
	}
}

// ---- PGVectorBackend builder (pure) ----

func TestBuildPGVectorStatefulSet_Image(t *testing.T) {
	q := resource.MustParse(pgvectorDefaultStorage)
	sts := buildPGVectorStatefulSet("mem", "ns", "mem-pgv", q)
	if sts.Spec.Template.Spec.Containers[0].Image != pgvectorFallbackImage {
		t.Errorf("wrong image: %q", sts.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestBuildPGVectorSvc_Port(t *testing.T) {
	svc := buildPGVectorSvc("mem", "ns", "mem-pgv")
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != pgvectorPort {
		t.Errorf("wrong port: %v", svc.Spec.Ports)
	}
}

// ---- Neo4jBackend ----

func TestNeo4jBackend_ExternalMode_ProvisionNoOp(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewNeo4jBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderNeo4j,
		Neo4j: &keesev1alpha1.Neo4jConfig{URI: "bolt://neo4j.default.svc:7687"},
	}
	created, err := b.Provision(context.Background(), provider, "mem-neo4j", "default")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if created {
		t.Error("external URI: created must be false")
	}
}

func TestNeo4jBackend_ExternalMode_Healthy(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewNeo4jBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderNeo4j,
		Neo4j: &keesev1alpha1.Neo4jConfig{URI: "bolt://neo4j.default.svc:7687"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-neo4j", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("external URI: should be healthy")
	}
}

// ---- Neo4jBackend builder (pure) ----

func TestBuildNeo4jStatefulSet_StorageSize(t *testing.T) {
	storageQ := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", storageQ)
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("expected 1 VolumeClaimTemplate, got %d", len(sts.Spec.VolumeClaimTemplates))
	}
	got := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
	if got.Cmp(storageQ) != 0 {
		t.Errorf("storage mismatch: want %v got %v", storageQ, got)
	}
}

func TestBuildNeo4jStatefulSet_Ports(t *testing.T) {
	q := resource.MustParse("5Gi")
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q)
	found := map[string]bool{}
	for _, p := range sts.Spec.Template.Spec.Containers[0].Ports {
		found[p.Name] = true
	}
	if !found["bolt"] || !found["http"] {
		t.Errorf("expected bolt+http ports, got %v", sts.Spec.Template.Spec.Containers[0].Ports)
	}
}

func TestBuildNeo4jSvc_Headless(t *testing.T) {
	svc := buildNeo4jSvc("mem", "ns", "mem-neo4j")
	if svc.Spec.ClusterIP != "None" {
		t.Errorf("expected headless, got ClusterIP=%q", svc.Spec.ClusterIP)
	}
}

func TestBuildNeo4jStatefulSet_Image(t *testing.T) {
	q := resource.MustParse("5Gi")
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q)
	if sts.Spec.Template.Spec.Containers[0].Image != neo4jImage {
		t.Errorf("wrong image: %q", sts.Spec.Template.Spec.Containers[0].Image)
	}
}

// ---- Mem0Backend ----

func TestMem0Backend_CredentialSecretRequired(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewMem0Backend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderMem0,
		Mem0: &keesev1alpha1.Mem0Config{}, // missing credentialSecretRef
	}
	_, err := b.Provision(context.Background(), provider, "mem-mem0", "default")
	if err == nil {
		t.Fatal("expected error for missing credentialSecretRef")
	}
}

func TestMem0Backend_HealthyWhenSecretPresent(t *testing.T) {
	s := newFakeScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mem0-creds", Namespace: "default"},
		Data:       map[string][]byte{"api_key": []byte("sk-test")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	b := NewMem0Backend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderMem0,
		Mem0: &keesev1alpha1.Mem0Config{CredentialSecretRef: "mem0-creds"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-mem0", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("secret present: should be healthy")
	}
}

func TestMem0Backend_HealthyWhenSecretMissing(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewMem0Backend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderMem0,
		Mem0: &keesev1alpha1.Mem0Config{CredentialSecretRef: "missing"},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-mem0", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if ok {
		t.Error("missing secret: should be unhealthy")
	}
}

func TestMem0Backend_DeprovisionNoOp_WhenNoESO(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewMem0Backend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderMem0,
		Mem0: &keesev1alpha1.Mem0Config{CredentialSecretRef: "mem0-creds"},
	}
	// Deprovision on an absent ExternalSecret must not error (idempotent).
	if err := b.Deprovision(context.Background(), provider, "mem-mem0", "default"); err != nil {
		t.Fatalf("deprovision: %v", err)
	}
}

// ---- ZepBackend ----

func TestZepBackend_CredentialSecretRequired(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewZepBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderZep,
		Zep:  &keesev1alpha1.ZepConfig{}, // no credentialSecretRef
	}
	_, err := b.Provision(context.Background(), provider, "mem-zep", "default")
	if err == nil {
		t.Fatal("expected error for missing credentialSecretRef")
	}
}

func TestZepBackend_CloudMode_HealthyWhenSecretPresent(t *testing.T) {
	s := newFakeScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "zep-creds", Namespace: "default"},
		Data:       map[string][]byte{"api_key": []byte("zep-token")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	b := NewZepBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderZep,
		Zep: &keesev1alpha1.ZepConfig{
			APIEndpoint:         "https://api.getzep.com",
			CredentialSecretRef: "zep-creds",
		},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-zep", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if !ok {
		t.Error("cloud mode + secret present: should be healthy")
	}
}

func TestZepBackend_CloudMode_HealthyWhenSecretMissing(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	b := NewZepBackend(c)

	provider := keesev1alpha1.MemoryProvider{
		Type: keesev1alpha1.ProviderZep,
		Zep: &keesev1alpha1.ZepConfig{
			APIEndpoint:         "https://api.getzep.com",
			CredentialSecretRef: "missing",
		},
	}
	ok, err := b.Healthy(context.Background(), provider, "mem-zep", "default")
	if err != nil {
		t.Fatalf("healthy: %v", err)
	}
	if ok {
		t.Error("missing secret: should be unhealthy")
	}
}

// ---- ZepBackend builder (pure) ----

func TestBuildZepStatefulSet_ProjectedCredentials(t *testing.T) {
	q := resource.MustParse(zepDefaultStorage)
	sts := buildZepStatefulSet("mem", "ns", "mem-zep", "zep-creds", q)

	found := false
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Name == "zep-creds" && v.Projected != nil {
			found = true
		}
	}
	if !found {
		t.Error("projected credential volume not found on zep StatefulSet")
	}
}

func TestBuildZepStatefulSet_CredentialMountedAsFile(t *testing.T) {
	q := resource.MustParse(zepDefaultStorage)
	sts := buildZepStatefulSet("mem", "ns", "mem-zep", "zep-creds", q)

	found := false
	for _, vm := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.Name == "zep-creds" && vm.MountPath == "/var/run/keese/secrets/zep" && vm.ReadOnly {
			found = true
		}
	}
	if !found {
		t.Error("credential must be mounted read-only at /var/run/keese/secrets/zep (rule 05.7)")
	}
}

// ---- MultiBackendProvisioner dispatch ----

func TestMultiBackendProvisioner_DispatchesByType(t *testing.T) {
	s := newFakeScheme(t)
	// All cases use external/no-op modes (no SSA needed) to avoid fake-client SSA limits.
	cases := []struct {
		name     string
		provider keesev1alpha1.MemoryProvider
	}{
		{
			name: "redis-external",
			provider: keesev1alpha1.MemoryProvider{
				Type:  keesev1alpha1.ProviderRedis,
				Redis: &keesev1alpha1.RedisConfig{Address: "redis:6379"},
			},
		},
		{
			name: "qdrant-external",
			provider: keesev1alpha1.MemoryProvider{
				Type:   keesev1alpha1.ProviderQdrant,
				Qdrant: &keesev1alpha1.QdrantConfig{CollectionName: "c", Endpoint: "qdrant:6334"},
			},
		},
		{
			name: "pgvector-external",
			provider: keesev1alpha1.MemoryProvider{
				Type:     keesev1alpha1.ProviderPGVector,
				PGVector: &keesev1alpha1.PGVectorConfig{DSNSecretRef: "pg-dsn"},
			},
		},
		{
			name: "neo4j-external",
			provider: keesev1alpha1.MemoryProvider{
				Type:  keesev1alpha1.ProviderNeo4j,
				Neo4j: &keesev1alpha1.Neo4jConfig{URI: "bolt://neo4j:7687"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(s).Build()
			m := NewMultiBackendProvisioner(c)
			// Provision in external mode must not return an error.
			if _, err := m.Provision(context.Background(), tc.provider, "mem-"+tc.name, "default"); err != nil {
				t.Fatalf("Provision(%s): %v", tc.name, err)
			}
			// Deprovision in external mode is a no-op.
			if err := m.Deprovision(context.Background(), tc.provider, "mem-"+tc.name, "default"); err != nil {
				t.Fatalf("Deprovision(%s): %v", tc.name, err)
			}
		})
	}
}

func TestMultiBackendProvisioner_UnknownType_ReturnsError(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	m := NewMultiBackendProvisioner(c)

	provider := keesev1alpha1.MemoryProvider{Type: "nonexistent"}
	_, err := m.Provision(context.Background(), provider, "mem", "default")
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
	if err2 := m.Deprovision(context.Background(), provider, "mem", "default"); err2 == nil {
		t.Fatal("expected error for unknown provider type on Deprovision")
	}
	if _, err3 := m.Healthy(context.Background(), provider, "mem", "default"); err3 == nil {
		t.Fatal("expected error for unknown provider type on Healthy")
	}
}

// ---- HAValidation (existing logic, kept here for completeness) ----

func TestValidateHA_Redis(t *testing.T) {
	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Replicas: 1},
	}
	// Non-dev namespace: expect error.
	if err := validateHA(provider, "production", nil); err == nil {
		t.Error("expected HA violation in production namespace")
	}
	// Dev namespace: no error.
	if err := validateHA(provider, "my-team-dev", nil); err != nil {
		t.Errorf("unexpected error in dev namespace: %v", err)
	}
	// Default namespace: no error.
	if err := validateHA(provider, "default", nil); err != nil {
		t.Errorf("unexpected error in default namespace: %v", err)
	}
}

func TestValidateHA_Qdrant(t *testing.T) {
	provider := keesev1alpha1.MemoryProvider{
		Type:   keesev1alpha1.ProviderQdrant,
		Qdrant: &keesev1alpha1.QdrantConfig{Replicas: 1},
	}
	if err := validateHA(provider, "staging", nil); err == nil {
		t.Error("expected HA violation in staging namespace")
	}
}

// keese.ai/env=dev label exempts a non-suffixed namespace.
func TestValidateHA_DevLabelExempts(t *testing.T) {
	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Replicas: 1},
	}
	labels := map[string]string{devNamespaceLabel: "dev"}
	if err := validateHA(provider, "production", labels); err != nil {
		t.Errorf("expected dev label to exempt HA check, got %v", err)
	}
}

// A non-"dev" label value falls through to the name heuristic; production
// with no -dev suffix still violates.
func TestValidateHA_NonDevLabelDoesNotExempt(t *testing.T) {
	provider := keesev1alpha1.MemoryProvider{
		Type:  keesev1alpha1.ProviderRedis,
		Redis: &keesev1alpha1.RedisConfig{Replicas: 1},
	}
	labels := map[string]string{devNamespaceLabel: "prod"}
	if err := validateHA(provider, "production", labels); err == nil {
		t.Error("expected HA violation when label != dev and name is non-dev")
	}
}

// ---- secretExists helper ----

func TestSecretExists_Present(t *testing.T) {
	s := newFakeScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"key": []byte("value")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	ok, err := secretExists(context.Background(), c, "my-secret", "default")
	if err != nil || !ok {
		t.Errorf("secretExists: ok=%v err=%v", ok, err)
	}
}

func TestSecretExists_Missing(t *testing.T) {
	s := newFakeScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ok, err := secretExists(context.Background(), c, "missing", "default")
	if err != nil || ok {
		t.Errorf("secretExists(missing): ok=%v err=%v", ok, err)
	}
}

func TestSecretExists_EmptyData(t *testing.T) {
	s := newFakeScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Data:       map[string][]byte{}, // empty data
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	ok, err := secretExists(context.Background(), c, "empty", "default")
	if err != nil || ok {
		t.Errorf("empty secret must not count as healthy: ok=%v err=%v", ok, err)
	}
}
