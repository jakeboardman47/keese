// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Unit tests asserting that backend StatefulSet builders wire credential Secrets
// as projected files (rule 05.7) and never as env vars.
//
// No //go:build tag — runs under `go test -short ./internal/controller/keese/`.
// No client needed: these are pure builder tests.

package keese

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// podSpecEnvVarSecretCheck walks every container in the pod spec and fails the
// test if any container uses env.valueFrom.secretKeyRef or envFrom referencing
// a Secret — both are forbidden by rule 05.7.
func podSpecEnvVarSecretCheck(t *testing.T, spec corev1.PodSpec) {
	t.Helper()
	for _, c := range spec.Containers {
		for _, e := range c.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				t.Errorf("container %q uses env.valueFrom.secretKeyRef for key %q — forbidden by rule 05.7", c.Name, e.Name)
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil {
				t.Errorf("container %q uses envFrom.secretRef %q — forbidden by rule 05.7", c.Name, ef.SecretRef.Name)
			}
		}
	}
}

// hasProjectedVolume returns true if the pod spec contains a projected volume
// whose name matches volName and that projects the named Secret.
func hasProjectedVolume(spec corev1.PodSpec, volName, secretRef string) bool {
	for _, v := range spec.Volumes {
		if v.Name != volName || v.Projected == nil {
			continue
		}
		for _, src := range v.Projected.Sources {
			if src.Secret != nil && src.Secret.Name == secretRef {
				return true
			}
		}
	}
	return false
}

// hasReadOnlyMount returns true if the named container has a VolumeMount at the
// given path that is read-only.
func hasReadOnlyMount(spec corev1.PodSpec, containerName, volName, mountPath string) bool {
	for _, c := range spec.Containers {
		if c.Name != containerName {
			continue
		}
		for _, vm := range c.VolumeMounts {
			if vm.Name == volName && vm.MountPath == mountPath && vm.ReadOnly {
				return true
			}
		}
	}
	return false
}

// hasCommandArg returns true if the named container's Args slice contains a
// string that includes the given substring.
func hasCommandArg(spec corev1.PodSpec, containerName, substr string) bool {
	for _, c := range spec.Containers {
		if c.Name != containerName {
			continue
		}
		for _, a := range c.Args {
			if strings.Contains(a, substr) {
				return true
			}
		}
	}
	return false
}

// hasEnvVar returns true if the named container has an Env entry with the given name.
func hasEnvVar(spec corev1.PodSpec, containerName, envName string) bool {
	for _, c := range spec.Containers {
		if c.Name != containerName {
			continue
		}
		for _, e := range c.Env {
			if e.Name == envName {
				return true
			}
		}
	}
	return false
}

// ---- Redis ----

func TestBuildRedisStatefulSet_WithCred_ProjectedVolume(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 1, "redis-secret")
	spec := sts.Spec.Template.Spec
	if !hasProjectedVolume(spec, "redis-creds", "redis-secret") {
		t.Error("projected volume for redis-creds not found")
	}
}

func TestBuildRedisStatefulSet_WithCred_MountPath(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 1, "redis-secret")
	spec := sts.Spec.Template.Spec
	if !hasReadOnlyMount(spec, "redis", "redis-creds", "/var/run/keese/secrets/redis") {
		t.Error("read-only mount at /var/run/keese/secrets/redis not found on redis container")
	}
}

func TestBuildRedisStatefulSet_WithCred_AuthCommand(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 1, "redis-secret")
	spec := sts.Spec.Template.Spec
	if !hasCommandArg(spec, "redis", "--requirepass") {
		t.Error("redis auth command must contain --requirepass")
	}
	if !hasCommandArg(spec, "redis", "/var/run/keese/secrets/redis/password") {
		t.Error("redis auth command must read password from projected file path")
	}
}

func TestBuildRedisStatefulSet_WithCred_NoSecretEnvVars(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 1, "redis-secret")
	podSpecEnvVarSecretCheck(t, sts.Spec.Template.Spec)
}

func TestBuildRedisStatefulSet_NoCred_NoProjectedVolume(t *testing.T) {
	sts := buildRedisStatefulSet("mem", "ns", "mem-redis", 1, "")
	spec := sts.Spec.Template.Spec
	for _, v := range spec.Volumes {
		if v.Name == "redis-creds" {
			t.Error("no credential projected volume expected when credSecretRef is empty")
		}
	}
	// No auth command either.
	if hasCommandArg(spec, "redis", "--requirepass") {
		t.Error("no auth command expected when credSecretRef is empty")
	}
}

// ---- Neo4j ----

func TestBuildNeo4jStatefulSet_WithCred_ProjectedVolume(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "neo4j-secret")
	spec := sts.Spec.Template.Spec
	if !hasProjectedVolume(spec, "neo4j-creds", "neo4j-secret") {
		t.Error("projected volume for neo4j-creds not found")
	}
}

func TestBuildNeo4jStatefulSet_WithCred_MountPath(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "neo4j-secret")
	spec := sts.Spec.Template.Spec
	if !hasReadOnlyMount(spec, "neo4j", "neo4j-creds", "/var/run/keese/secrets/neo4j") {
		t.Error("read-only mount at /var/run/keese/secrets/neo4j not found on neo4j container")
	}
}

func TestBuildNeo4jStatefulSet_WithCred_AuthCommand(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "neo4j-secret")
	spec := sts.Spec.Template.Spec
	if !hasCommandArg(spec, "neo4j", "NEO4J_AUTH") {
		t.Error("neo4j auth command must set NEO4J_AUTH")
	}
	if !hasCommandArg(spec, "neo4j", "/var/run/keese/secrets/neo4j/password") {
		t.Error("neo4j auth command must read password from projected file path")
	}
	if !hasCommandArg(spec, "neo4j", "/startup/docker-entrypoint.sh") {
		t.Error("neo4j auth command must exec /startup/docker-entrypoint.sh")
	}
}

func TestBuildNeo4jStatefulSet_WithCred_NoNeo4jAuthNoneEnv(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "neo4j-secret")
	spec := sts.Spec.Template.Spec
	if hasEnvVar(spec, "neo4j", "NEO4J_AUTH") {
		t.Error("NEO4J_AUTH env var must not be set when credSecretRef is provided (it appears in the command instead)")
	}
}

func TestBuildNeo4jStatefulSet_WithCred_NoSecretEnvVars(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "neo4j-secret")
	podSpecEnvVarSecretCheck(t, sts.Spec.Template.Spec)
}

func TestBuildNeo4jStatefulSet_NoCred_HasNeo4jAuthNone(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "")
	spec := sts.Spec.Template.Spec
	if !hasEnvVar(spec, "neo4j", "NEO4J_AUTH") {
		t.Error("NEO4J_AUTH=none env var must be present in no-cred (dev) mode")
	}
	// Confirm it is "none" not a secret reference.
	for _, c := range spec.Containers {
		if c.Name != "neo4j" {
			continue
		}
		for _, e := range c.Env {
			if e.Name == "NEO4J_AUTH" && e.Value != "none" {
				t.Errorf("NEO4J_AUTH must be 'none' in dev mode, got %q", e.Value)
			}
		}
	}
}

func TestBuildNeo4jStatefulSet_NoCred_NoProjectedVolume(t *testing.T) {
	q := resource.MustParse(neo4jDefaultStorage)
	sts := buildNeo4jStatefulSet("mem", "ns", "mem-neo4j", q, "")
	spec := sts.Spec.Template.Spec
	for _, v := range spec.Volumes {
		if v.Name == "neo4j-creds" {
			t.Error("no credential projected volume expected when credSecretRef is empty")
		}
	}
}

// ---- Qdrant ----

func TestBuildQdrantStatefulSet_WithCred_ProjectedVolume(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "qdrant-secret")
	spec := sts.Spec.Template.Spec
	if !hasProjectedVolume(spec, "qdrant-creds", "qdrant-secret") {
		t.Error("projected volume for qdrant-creds not found")
	}
}

func TestBuildQdrantStatefulSet_WithCred_MountPath(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "qdrant-secret")
	spec := sts.Spec.Template.Spec
	if !hasReadOnlyMount(spec, "qdrant", "qdrant-creds", "/var/run/keese/secrets/qdrant") {
		t.Error("read-only mount at /var/run/keese/secrets/qdrant not found on qdrant container")
	}
}

func TestBuildQdrantStatefulSet_WithCred_AuthCommand(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "qdrant-secret")
	spec := sts.Spec.Template.Spec
	if !hasCommandArg(spec, "qdrant", "QDRANT__SERVICE__API_KEY") {
		t.Error("qdrant auth command must set QDRANT__SERVICE__API_KEY")
	}
	if !hasCommandArg(spec, "qdrant", "/var/run/keese/secrets/qdrant/api-key") {
		t.Error("qdrant auth command must read api-key from projected file path")
	}
	if !hasCommandArg(spec, "qdrant", "./entrypoint.sh") {
		t.Error("qdrant auth command must exec ./entrypoint.sh")
	}
}

func TestBuildQdrantStatefulSet_WithCred_WorkingDir(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "qdrant-secret")
	spec := sts.Spec.Template.Spec
	for _, c := range spec.Containers {
		if c.Name == "qdrant" && c.WorkingDir != "/qdrant" {
			t.Errorf("qdrant container WorkingDir must be /qdrant, got %q", c.WorkingDir)
		}
	}
}

func TestBuildQdrantStatefulSet_WithCred_NoSecretEnvVars(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "qdrant-secret")
	podSpecEnvVarSecretCheck(t, sts.Spec.Template.Spec)
}

func TestBuildQdrantStatefulSet_NoCred_NoProjectedVolume(t *testing.T) {
	q := resource.MustParse(qdrantDefaultStorage)
	sts := buildQdrantStatefulSet("mem", "ns", "mem-qdrant", 1, q, "")
	spec := sts.Spec.Template.Spec
	for _, v := range spec.Volumes {
		if v.Name == "qdrant-creds" {
			t.Error("no credential projected volume expected when credSecretRef is empty")
		}
	}
	if hasCommandArg(spec, "qdrant", "QDRANT__SERVICE__API_KEY") {
		t.Error("no auth command expected when credSecretRef is empty")
	}
}

// ---- cross-backend: rule 05.7 invariant on no-cred path ----

func TestAllBackends_NoCred_NoSecretEnvVars(t *testing.T) {
	q := resource.MustParse("1Gi")
	cases := []struct {
		name string
		spec corev1.PodSpec
	}{
		{"redis", buildRedisStatefulSet("m", "ns", "m-redis", 1, "").Spec.Template.Spec},
		{"neo4j", buildNeo4jStatefulSet("m", "ns", "m-neo4j", q, "").Spec.Template.Spec},
		{"qdrant", buildQdrantStatefulSet("m", "ns", "m-qdrant", 1, q, "").Spec.Template.Spec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			podSpecEnvVarSecretCheck(t, tc.spec)
		})
	}
}
