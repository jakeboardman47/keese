// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package featuregate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/prometheus/client_golang/prometheus"
)

// Gate is the typed name of a feature gate. Values must match the
// CR `metadata.name` of the corresponding FeatureGate cluster
// resource (DNS-1035; lowercase + hyphens).
type Gate string

// Stable gate IDs used across the codebase. Add new entries here +
// docs/designs/27b-feature-gate-catalog.md when introducing a gate.
const (
	CosignInstallPlanVerify     Gate = "cosign-installplan-verify"
	CosignInstallPlanFailClosed Gate = "cosign-installplan-failclosed"
)

// Options configures a Gates instance.
type Options struct {
	// Path is the JSON file the controller projects to. Defaults to
	// /etc/keese/features/gates.json.
	Path string

	// Defaults seeds the per-gate fallback values used when the
	// projection file is missing, malformed, or has no entry for a
	// requested gate. Callers should populate Defaults from
	// api/policy/v1alpha1.DefaultEffective(stage) for every gate
	// they consume.
	Defaults map[Gate]bool

	// Binary names this consumer for Prometheus labels + the
	// Status.Consumers ring buffer. Defaults to filepath.Base of the
	// running executable.
	Binary string

	// Log is the logger used for transition + reload events. Defaults
	// to a discard logger.
	Log logr.Logger

	// Registerer receives the eval counter. Defaults to
	// prometheus.DefaultRegisterer; pass a custom one in tests to
	// avoid duplicate-collector panics.
	Registerer prometheus.Registerer
}

// Gates is the live evaluator. Start it once per process; reads via
// Enabled are lock-free.
type Gates struct {
	values     atomic.Pointer[map[Gate]bool]
	defaults   map[Gate]bool
	binary     string
	log        logr.Logger
	loader     *fileLoader
	client     *openfeature.Client
	provider   *inProcessProvider
	evalCount  *prometheus.CounterVec
	stateGauge *prometheus.GaugeVec
}

const defaultPath = "/etc/keese/features/gates.json"

// New starts a Gates instance. The first read from the projection
// file is synchronous; subsequent reloads are background (fsnotify).
// Failure to read the file is non-fatal — Defaults take effect.
func New(ctx context.Context, opts Options) (*Gates, error) {
	if opts.Path == "" {
		opts.Path = defaultPath
	}
	if opts.Binary == "" {
		opts.Binary = filepath.Base(opts.Path)
	}
	if opts.Defaults == nil {
		opts.Defaults = map[Gate]bool{}
	}

	g := &Gates{
		defaults: copyMap(opts.Defaults),
		binary:   opts.Binary,
		log:      opts.Log,
	}

	// Seed values with defaults so reads before the loader
	// completes still succeed.
	seed := copyMap(opts.Defaults)
	g.values.Store(&seed)

	// Metrics (registered into the supplied registerer).
	reg := opts.Registerer
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	g.evalCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keese_featuregate_eval_total",
			Help: "Number of feature-gate evaluations, labeled by gate, value, and binary.",
		},
		[]string{"gate", "value", "binary"},
	)
	g.stateGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "keese_featuregate_state",
			Help: "Current effective value of each known feature gate (0=off, 1=on).",
		},
		[]string{"gate"},
	)
	if err := registerOnce(reg, g.evalCount, g.stateGauge); err != nil {
		return nil, err
	}

	// OpenFeature provider wired to our in-process map.
	g.provider = &inProcessProvider{gates: g}
	if err := openfeature.SetProviderAndWait(g.provider); err != nil {
		return nil, fmt.Errorf("openfeature set provider: %w", err)
	}
	g.client = openfeature.NewClient(opts.Binary)

	// Initial load — best-effort.
	loader, err := newFileLoader(opts.Path, g.applySnapshot, g.log)
	if err != nil {
		return nil, fmt.Errorf("featuregate file loader: %w", err)
	}
	g.loader = loader
	if err := loader.start(ctx); err != nil {
		return nil, fmt.Errorf("start loader: %w", err)
	}
	return g, nil
}

// Close stops the file watcher. Idempotent.
func (g *Gates) Close() error {
	if g == nil || g.loader == nil {
		return nil
	}
	return g.loader.close()
}

// Enabled returns the current effective value of the named gate.
// Lock-free hot path: a single atomic load + map lookup. Records a
// counter under (gate, value, binary). When the gate is unknown,
// the per-gate default is returned (or false if no default is set).
func (g *Gates) Enabled(ctx context.Context, gate Gate) bool {
	v := g.lookup(gate)
	g.evalCount.WithLabelValues(string(gate),
		boolStr(v), g.binary).Inc()
	return v
}

// Snapshot returns a copy of the current effective gate map. Useful
// for /debug endpoints + tests.
func (g *Gates) Snapshot() map[Gate]bool {
	return copyMap(*g.values.Load())
}

// applySnapshot replaces the in-memory map with a parsed projection
// file's contents. Any gate present in Defaults but missing from the
// file falls back to its default. Called on initial load + every
// fsnotify reload.
func (g *Gates) applySnapshot(parsed map[string]bool) {
	merged := make(map[Gate]bool, len(g.defaults)+len(parsed))
	for k, v := range g.defaults {
		merged[k] = v
	}
	for k, v := range parsed {
		merged[Gate(k)] = v
	}
	g.values.Store(&merged)
	for k, v := range merged {
		g.stateGauge.WithLabelValues(string(k)).Set(boolFloat(v))
	}
}

func (g *Gates) lookup(gate Gate) bool {
	m := g.values.Load()
	if m != nil {
		if v, ok := (*m)[gate]; ok {
			return v
		}
	}
	if v, ok := g.defaults[gate]; ok {
		return v
	}
	return false
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func copyMap(m map[Gate]bool) map[Gate]bool {
	out := make(map[Gate]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// registerOnce registers each collector and tolerates
// AlreadyRegisteredError so a process re-entering New() in tests
// doesn't panic.
func registerOnce(reg prometheus.Registerer, cs ...prometheus.Collector) error {
	for _, c := range cs {
		if err := reg.Register(c); err != nil {
			are := prometheus.AlreadyRegisteredError{}
			if !errors.As(err, &are) {
				return err
			}
		}
	}
	return nil
}

// inProcessProvider is the OpenFeature provider that delegates to
// Gates. Booleans are the only evaluation type we use; other
// kinds return unsupported.
type inProcessProvider struct {
	gates *Gates
	once  sync.Once
}

func (p *inProcessProvider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "keese-featuregate-inprocess"}
}

func (p *inProcessProvider) Hooks() []openfeature.Hook { return nil }

func (p *inProcessProvider) BooleanEvaluation(_ context.Context, flag string,
	defaultValue bool, _ openfeature.FlattenedContext,
) openfeature.BoolResolutionDetail {
	v, ok := (*p.gates.values.Load())[Gate(flag)]
	if !ok {
		v = defaultValue
	}
	return openfeature.BoolResolutionDetail{
		Value: v,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: openfeature.StaticReason,
		},
	}
}

func (p *inProcessProvider) StringEvaluation(_ context.Context, _ string,
	defaultValue string, _ openfeature.FlattenedContext,
) openfeature.StringResolutionDetail {
	return openfeature.StringResolutionDetail{
		Value: defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: openfeature.ErrorReason,
			ResolutionError: openfeature.NewGeneralResolutionError(
				"keese featuregate provider supports boolean only"),
		},
	}
}

func (p *inProcessProvider) FloatEvaluation(_ context.Context, _ string,
	defaultValue float64, _ openfeature.FlattenedContext,
) openfeature.FloatResolutionDetail {
	return openfeature.FloatResolutionDetail{
		Value: defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: openfeature.ErrorReason,
			ResolutionError: openfeature.NewGeneralResolutionError(
				"keese featuregate provider supports boolean only"),
		},
	}
}

func (p *inProcessProvider) IntEvaluation(_ context.Context, _ string,
	defaultValue int64, _ openfeature.FlattenedContext,
) openfeature.IntResolutionDetail {
	return openfeature.IntResolutionDetail{
		Value: defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: openfeature.ErrorReason,
			ResolutionError: openfeature.NewGeneralResolutionError(
				"keese featuregate provider supports boolean only"),
		},
	}
}

func (p *inProcessProvider) ObjectEvaluation(_ context.Context, _ string,
	defaultValue any, _ openfeature.FlattenedContext,
) openfeature.InterfaceResolutionDetail {
	return openfeature.InterfaceResolutionDetail{
		Value: defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: openfeature.ErrorReason,
			ResolutionError: openfeature.NewGeneralResolutionError(
				"keese featuregate provider supports boolean only"),
		},
	}
}
