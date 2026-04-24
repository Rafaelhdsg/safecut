package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Rafaelhdsg/inframind-cli/internal/discovery"
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/forecast"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
	"github.com/Rafaelhdsg/inframind-cli/internal/pricing"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
	"github.com/Rafaelhdsg/inframind-cli/internal/rules"
	"github.com/Rafaelhdsg/inframind-cli/internal/simulation"
)

// Output is the final result produced by running the full pipeline.
type Output struct {
	Snapshot    *discovery.Snapshot
	Graph       *graph.DependencyGraph
	Analyses    map[string]*engine.IdleAnalysis
	Policies    map[string]*engine.ResolvedPolicy
	Decisions   []engine.Recommendation
	Simulation  simulation.Result
	Forecast    forecast.Projection
	Protected   []engine.ProtectedResource
	Observed    []engine.ObservedResource
	Drifts      []engine.PolicyDrift
	InvalidTags []InvalidTag
	// PricingWarnings lists human-readable regions/services where the
	// Retail Prices API was unavailable. Every monetary number the
	// user sees for those regions should be treated as a fallback and
	// is excluded from TotalSaving by the rule-level fail-closed logic.
	PricingWarnings []string
	// TagsWarnings reports failed hierarchy-tag lookups (subscription
	// / resource group). Policy resolution silently falls back to
	// resource-level tags when these fail, so surfacing the warning
	// gives the operator a chance to notice unexpected governance
	// behaviour.
	TagsWarnings []string
}

// ProgressFunc mirrors discovery.ProgressFunc to avoid circular imports.
type ProgressFunc func(stage, detail string, current, total int)

// Pipeline orchestrates InfraMind's layers:
// Discovery -> Policy Resolution (with inheritance) -> Partition -> Graph -> Analysis -> Decision -> Simulation -> Forecast.
type Pipeline struct {
	provider   providers.Provider
	analyzer   *engine.Analyzer
	resolver   *engine.PolicyResolver
	rules      []rules.Rule
	pricing    pricing.PricingProvider
	OnProgress ProgressFunc
	// ResourceGroup, if set, limits discovery to resources in this resource group (name match, case-insensitive).
	ResourceGroup string
}

// SetPricing attaches a PricingProvider for accurate cost resolution.
func (p *Pipeline) SetPricing(pp pricing.PricingProvider) {
	p.pricing = pp
}

func (p *Pipeline) progress(stage, detail string) {
	if p.OnProgress != nil {
		p.OnProgress(stage, detail, 0, 0)
	}
}

func New(p providers.Provider) *Pipeline {
	return &Pipeline{
		provider: p,
		analyzer: engine.DefaultAnalyzer(),
		resolver: engine.NewPolicyResolver(),
		rules: []rules.Rule{
			&rules.OrphanDiskRule{},
			&rules.OrphanIPRule{},
			rules.DefaultIdleResourceRule(),
			rules.DefaultRightsizeRule(),
			rules.DefaultReservedInstanceRule(),
			&rules.IdleAppServiceRule{},
			&rules.IdleSQLDatabaseRule{},
			&rules.IdleStorageAccountRule{},
			&rules.OrphanLoadBalancerRule{},
			&rules.OrphanNATGatewayRule{},
			&rules.IdleContainerGroupRule{},
		},
	}
}

// Run executes the full analysis pipeline end-to-end.
func (p *Pipeline) Run(ctx context.Context, resourceTypes []string, forecastMonths int) (*Output, error) {
	// Layer 1: Discovery — collect resources, metrics, and hierarchy tags
	collector := discovery.NewCollector(p.provider)
	collector.ResourceGroupFilter = p.ResourceGroup
	if p.OnProgress != nil {
		collector.OnProgress = discovery.ProgressFunc(p.OnProgress)
	}
	snap, err := collector.Collect(ctx, resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	// Layer 1.1: Pricing — warm up retail prices for discovered regions.
	// Warmup failures are NOT fatal (the CLI must still run in
	// air-gapped / rate-limited environments), but every region that
	// failed is surfaced in PricingWarnings so the output layer can
	// warn the user and so rules that require a real price can refuse
	// to emit savings numbers for those regions.
	var pricingWarnings []string
	if p.pricing != nil {
		regions := uniqueRegions(snap.Resources)
		p.progress("Loading real prices...", fmt.Sprintf("%d regions", len(regions)))
		for _, region := range regions {
			if err := p.pricing.Warmup(ctx, region); err != nil {
				pricingWarnings = append(pricingWarnings,
					fmt.Sprintf("region %q: %v", region, err))
			}
		}
	}

	// Layer 1.5: Policy Resolution — walk tag hierarchy with inheritance
	p.progress("Resolving policies...", fmt.Sprintf("%d resources", len(snap.Resources)))
	policies := p.resolvePolicies(snap)

	// Collect all drift warnings
	var allDrifts []engine.PolicyDrift
	for _, pol := range policies {
		allDrifts = append(allDrifts, pol.Drifts...)
	}

	// Layer 1.6: Partition by mode
	eligible, protected := partitionByMode(snap.Resources, policies)
	snap.Resources = eligible

	// Layer 2: Dependency Graph — register all resources as nodes, then
	// discover real parent-child relationships from resource properties.
	p.progress("Building dependency graph...", "")
	dg := graph.NewDependencyGraph()
	allResources := append(snap.Resources, protectedAsResources(protected)...)
	for _, r := range allResources {
		dg.AddNode(&graph.Node{
			ID:   r.ID,
			Type: r.Type,
			Name: r.Name,
		})
	}
	graph.LinkAzureResources(dg, allResources)

	// Layer 2.5: Analysis with policy-aware thresholds
	p.progress("Analyzing idle scores...", fmt.Sprintf("%d eligible resources", len(snap.Resources)))
	analyses := p.analyzeMetrics(snap.Metrics, snap.Resources, policies)

	// Collect observe-mode resources
	observed := collectObserved(snap.Resources, policies, analyses)

	// Filter to actionable only
	actionable := filterObserved(snap.Resources, policies)

	// Layer 3: Decision Engine
	p.progress("Running decision engine...", "")
	de := engine.NewDecisionEngine()
	evalCtx := rules.EvalContext{
		Resources:               actionable,
		Metrics:                 snap.Metrics,
		Analyses:                analyses,
		Policies:                policies,
		Graph:                   dg,
		Locks:                   snap.Locks,
		Snapshots:               snap.Snapshots,
		Pricing:                 p.pricing,
		LocksStatus:             snap.LocksStatus,
		SnapshotsStatus:         snap.SnapshotsStatus,
		SafetyProviderSupported: snap.SafetyProviderSupported,
		PricingWarnings:         &pricingWarnings,
	}
	for _, rule := range p.rules {
		for _, rec := range rule.Evaluate(evalCtx) {
			de.AddRecommendation(rec)
		}
	}

	// Layer 4: Simulation Engine
	p.progress("Simulating actions...", "dependency safety check")
	sim := simulation.NewEngine(de, dg)
	simResult := sim.Run()

	// Layer 5: Forecast Engine
	p.progress("Projecting savings...", fmt.Sprintf("%d-month forecast", forecastMonths))
	proj := forecast.Calculate(simResult, forecastMonths)

	return &Output{
		Snapshot:        snap,
		Graph:           dg,
		Analyses:        analyses,
		Policies:        policies,
		Decisions:       de.Recommendations(),
		Simulation:      simResult,
		Forecast:        proj,
		Protected:       protected,
		Observed:        observed,
		Drifts:          allDrifts,
		InvalidTags:     detectInvalidTags(snap.Resources),
		PricingWarnings: pricingWarnings,
		TagsWarnings:    snap.TagsWarnings,
	}, nil
}

// LintResult is returned by policy lint — no metrics, no rules, no simulation.
// Useful for CI gating and pre-scan governance checks.
type LintResult struct {
	SubscriptionName string
	TotalResources   int
	Policies         map[string]*engine.ResolvedPolicy
	Drifts           []engine.PolicyDrift
	InvalidTags      []InvalidTag
	Resources        []providers.Resource
}

// InvalidTag captures a resource-level tag whose value does not match the
// accepted vocabulary for the canonical inframind-* keys.
type InvalidTag struct {
	ResourceID string
	Key        string
	Value      string
	Reason     string
}

// Lint runs discovery + policy resolution only. It skips metrics, rules,
// simulation, and forecast — cheap enough to run in CI on every PR.
func (p *Pipeline) Lint(ctx context.Context, resourceTypes []string) (*LintResult, error) {
	collector := discovery.NewCollector(p.provider)
	collector.ResourceGroupFilter = p.ResourceGroup
	collector.SkipMetrics = true
	if p.OnProgress != nil {
		collector.OnProgress = discovery.ProgressFunc(p.OnProgress)
	}
	snap, err := collector.Collect(ctx, resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	policies := p.resolvePolicies(snap)
	var allDrifts []engine.PolicyDrift
	for _, pol := range policies {
		allDrifts = append(allDrifts, pol.Drifts...)
	}

	invalid := detectInvalidTags(snap.Resources)

	return &LintResult{
		SubscriptionName: snap.SubscriptionName,
		TotalResources:   len(snap.Resources),
		Policies:         policies,
		Drifts:           allDrifts,
		InvalidTags:      invalid,
		Resources:        snap.Resources,
	}, nil
}

// detectInvalidTags flags resources whose inframind-* tags carry values
// the PolicyResolver does not understand.
func detectInvalidTags(resources []providers.Resource) []InvalidTag {
	validModes := map[string]bool{
		"default": true, "protect": true, "observe": true, "ignore": true, "": true,
	}
	validCrit := map[string]bool{
		"": true, "low": true, "medium": true, "high": true, "critical": true,
	}
	var out []InvalidTag
	builtins := engine.BuiltinTemplates
	for _, r := range resources {
		for k, v := range r.Tags {
			lk := strings.ToLower(strings.TrimSpace(k))
			lv := strings.ToLower(strings.TrimSpace(v))
			switch lk {
			case "inframind-mode", "inframind:mode":
				if !validModes[lv] {
					out = append(out, InvalidTag{ResourceID: r.ID, Key: k, Value: v,
						Reason: "mode must be one of: default, protect, observe, ignore"})
				}
			case "inframind-criticality", "inframind:criticality":
				if !validCrit[lv] {
					out = append(out, InvalidTag{ResourceID: r.ID, Key: k, Value: v,
						Reason: "criticality must be one of: low, medium, high, critical"})
				}
			case "inframind-template", "inframind:template":
				if _, ok := builtins[lv]; !ok && lv != "" {
					out = append(out, InvalidTag{ResourceID: r.ID, Key: k, Value: v,
						Reason: "unknown template (builtin: production, staging, development, legacy)"})
				}
			case "inframind-external", "inframind:external":
				if lv != "true" && lv != "false" && lv != "yes" && lv != "no" && lv != "" {
					out = append(out, InvalidTag{ResourceID: r.ID, Key: k, Value: v,
						Reason: "external must be true or false"})
				}
			}
		}
	}
	return out
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

func protectedAsResources(protected []engine.ProtectedResource) []providers.Resource {
	res := make([]providers.Resource, len(protected))
	for i, p := range protected {
		res[i] = providers.Resource{
			ID:   p.ResourceID,
			Name: p.ResourceName,
			Type: p.ResourceType,
		}
	}
	return res
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

func uniqueRegions(resources []providers.Resource) []string {
	seen := make(map[string]bool)
	var regions []string
	for _, r := range resources {
		loc := r.Location
		if loc != "" && !seen[loc] {
			seen[loc] = true
			regions = append(regions, loc)
		}
	}
	return regions
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

		// If metrics collection failed or returned no data points, we
		// refuse to produce an analysis. Rules key off analyses[id]
		// presence and will simply skip this resource, which is the
		// correct behaviour — better a missed recommendation than a
		// false "idle — auto-stop safe" on a VM whose Monitor query
		// was rate-limited or whose agent is down.
		if m.Status == discovery.MetricsDenied {
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
			DiskReadBytes:  m.DiskReadBytes,
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
