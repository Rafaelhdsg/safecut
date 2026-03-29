package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/Rafaelhdsg/inframind-cli/internal/discovery"
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/forecast"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
	"github.com/Rafaelhdsg/inframind-cli/internal/rules"
	"github.com/Rafaelhdsg/inframind-cli/internal/simulation"
)

// Output is the final result produced by running the full pipeline.
type Output struct {
	Snapshot   *discovery.Snapshot
	Graph      *graph.DependencyGraph
	Analyses   map[string]*engine.IdleAnalysis
	Policies   map[string]*engine.ResolvedPolicy
	Decisions  []engine.Recommendation
	Simulation simulation.Result
	Forecast   forecast.Projection
	Protected  []engine.ProtectedResource
	Observed   []engine.ObservedResource
	Drifts     []engine.PolicyDrift
}

// Pipeline orchestrates InfraMind's layers:
// Discovery -> Policy Resolution (with inheritance) -> Partition -> Graph -> Analysis -> Decision -> Simulation -> Forecast.
type Pipeline struct {
	provider providers.Provider
	analyzer *engine.Analyzer
	resolver *engine.PolicyResolver
	rules    []rules.Rule
}

func New(p providers.Provider) *Pipeline {
	return &Pipeline{
		provider: p,
		analyzer: engine.DefaultAnalyzer(),
		resolver: engine.NewPolicyResolver(),
		rules: []rules.Rule{
			&rules.OrphanDiskRule{},
			rules.DefaultIdleResourceRule(),
		},
	}
}

func NewWithConfig(p providers.Provider, a *engine.Analyzer, r *engine.PolicyResolver, rl []rules.Rule) *Pipeline {
	return &Pipeline{provider: p, analyzer: a, resolver: r, rules: rl}
}

// Run executes the full analysis pipeline end-to-end.
func (p *Pipeline) Run(ctx context.Context, resourceTypes []string, forecastMonths int) (*Output, error) {
	// Layer 1: Discovery — collect resources, metrics, and hierarchy tags
	collector := discovery.NewCollector(p.provider)
	snap, err := collector.Collect(ctx, resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	// Layer 1.5: Policy Resolution — walk tag hierarchy with inheritance
	policies := p.resolvePolicies(snap)

	// Collect all drift warnings
	var allDrifts []engine.PolicyDrift
	for _, pol := range policies {
		allDrifts = append(allDrifts, pol.Drifts...)
	}

	// Layer 1.6: Partition by mode
	eligible, protected := partitionByMode(snap.Resources, policies)
	snap.Resources = eligible

	// Layer 2: Dependency Graph
	dg := graph.NewDependencyGraph()
	for _, r := range snap.Resources {
		dg.AddNode(&graph.Node{
			ID:   r.ID,
			Type: r.Type,
			Name: r.Name,
		})
	}

	// Layer 2.5: Analysis with policy-aware thresholds
	analyses := p.analyzeMetrics(snap.Metrics, snap.Resources, policies)

	// Collect observe-mode resources
	observed := collectObserved(snap.Resources, policies, analyses)

	// Filter to actionable only
	actionable := filterObserved(snap.Resources, policies)

	// Layer 3: Decision Engine
	de := engine.NewDecisionEngine()
	evalCtx := rules.EvalContext{
		Resources: actionable,
		Metrics:   snap.Metrics,
		Analyses:  analyses,
		Policies:  policies,
	}
	for _, rule := range p.rules {
		for _, rec := range rule.Evaluate(evalCtx) {
			de.AddRecommendation(rec)
		}
	}

	// Layer 4: Simulation Engine
	sim := simulation.NewEngine(de, dg)
	simResult := sim.Run()

	// Layer 5: Forecast Engine
	proj := forecast.Calculate(simResult, forecastMonths)

	return &Output{
		Snapshot:   snap,
		Graph:      dg,
		Analyses:   analyses,
		Policies:   policies,
		Decisions:  de.Recommendations(),
		Simulation: simResult,
		Forecast:   proj,
		Protected:  protected,
		Observed:   observed,
		Drifts:     allDrifts,
	}, nil
}

// resolvePolicies builds a TagHierarchy per resource and resolves
// policies with full inheritance (resource → RG → subscription → default).
func (p *Pipeline) resolvePolicies(snap *discovery.Snapshot) map[string]*engine.ResolvedPolicy {
	policies := make(map[string]*engine.ResolvedPolicy, len(snap.Resources))
	for _, r := range snap.Resources {
		h := engine.TagHierarchy{
			ResourceTags:      r.Tags,
			ResourceGroupTags: snap.ResourceGroupTags[r.ResourceGroup],
			ResourceGroupName: r.ResourceGroup,
			SubscriptionTags:  snap.SubscriptionTags,
			SubscriptionName:  snap.SubscriptionName,
		}
		policies[r.ID] = p.resolver.Resolve(h)
	}
	return policies
}

func partitionByMode(resources []providers.Resource, policies map[string]*engine.ResolvedPolicy) (eligible []providers.Resource, protected []engine.ProtectedResource) {
	for _, r := range resources {
		pol := policies[r.ID]
		if pol != nil && pol.Mode == engine.ModeIgnore {
			protected = append(protected, engine.ProtectedResource{
				ResourceID:   r.ID,
				ResourceName: r.Name,
				ResourceType: r.Type,
				Policy:       pol,
			})
			continue
		}
		eligible = append(eligible, r)
	}
	return eligible, protected
}

func collectObserved(resources []providers.Resource, policies map[string]*engine.ResolvedPolicy, analyses map[string]*engine.IdleAnalysis) []engine.ObservedResource {
	var observed []engine.ObservedResource
	for _, r := range resources {
		pol := policies[r.ID]
		if pol == nil || pol.Mode != engine.ModeObserve {
			continue
		}
		observed = append(observed, engine.ObservedResource{
			ResourceID:   r.ID,
			ResourceName: r.Name,
			ResourceType: r.Type,
			Analysis:     analyses[r.ID],
		})
	}
	return observed
}

func filterObserved(resources []providers.Resource, policies map[string]*engine.ResolvedPolicy) []providers.Resource {
	var actionable []providers.Resource
	for _, r := range resources {
		pol := policies[r.ID]
		if pol != nil && pol.Mode == engine.ModeObserve {
			continue
		}
		actionable = append(actionable, r)
	}
	return actionable
}

func (p *Pipeline) analyzeMetrics(metrics map[string]*discovery.ResourceMetrics, eligible []providers.Resource, policies map[string]*engine.ResolvedPolicy) map[string]*engine.IdleAnalysis {
	eligibleIDs := make(map[string]bool, len(eligible))
	for _, r := range eligible {
		eligibleIDs[r.ID] = true
	}

	analyses := make(map[string]*engine.IdleAnalysis, len(eligible))
	now := time.Now()

	for id, m := range metrics {
		if !eligibleIDs[id] {
			continue
		}

		observed := time.Duration(0)
		if !m.LastActive.IsZero() {
			observed = now.Sub(m.LastActive)
		}

		input := engine.MetricInput{
			CPUAvgPercent:  m.CPUAvgPercent,
			NetworkIn:      m.NetworkIn,
			NetworkOut:     m.NetworkOut,
			DiskWriteBytes: m.DiskWriteBytes,
			LastActive:     m.LastActive,
		}

		policy := engine.DefaultPolicy()
		if pol := policies[id]; pol != nil {
			policy = pol.ResourcePolicy
		}

		analysis := p.analyzer.AnalyzeWithPolicy(input, observed, policy)
		analyses[id] = &analysis
	}
	return analyses
}
