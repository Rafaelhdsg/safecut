package pipeline

import (
	"context"
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/discovery"
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/forecast"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
	"github.com/Rafaelhdsg/inframind-cli/internal/rules"
	"github.com/Rafaelhdsg/inframind-cli/internal/simulation"
)

// PolicySimResult is the full what-if output: policy diff, decision diff,
// simulation results, forecast, impact score, and safety recommendation.
type PolicySimResult struct {
	*engine.PolicySimOutput

	// Full pipeline results for before and after
	BeforeSim      simulation.Result   `json:"before_simulation"`
	AfterSim       simulation.Result   `json:"after_simulation"`
	BeforeForecast forecast.Projection `json:"before_forecast"`
	AfterForecast  forecast.Projection `json:"after_forecast"`

	// Decision-level diff per resource
	DecisionDiffs []engine.DecisionDiff `json:"decision_diffs"`

	// Aggregated counts
	BeforeRecsCount int     `json:"before_recs_count"`
	AfterRecsCount  int     `json:"after_recs_count"`
	BeforeSavings   float64 `json:"before_savings"`
	AfterSavings    float64 `json:"after_savings"`
	SavingsDelta    float64 `json:"savings_delta"`

	// Blast radius: external dependency detection
	ExternalAffected int `json:"external_affected"`

	// Impact assessment
	Impact  engine.ImpactLevel `json:"impact_score"`
	Safety  string             `json:"safety_recommendation"`
	Summary string             `json:"summary"`
}

// SimulatePolicy runs the full pipeline twice (before and after hypothetical
// policy change) including simulation engine + forecast, then diffs everything.
func (p *Pipeline) SimulatePolicy(ctx context.Context, resourceTypes []string, input engine.PolicySimInput, forecastMonths int) (*PolicySimResult, error) {
	collector := discovery.NewCollector(p.provider)
	if input.Scope == engine.ScopeResourceGroup && input.ScopeName != "" {
		collector.ResourceGroupFilter = input.ScopeName
	}
	if p.OnProgress != nil {
		collector.OnProgress = discovery.ProgressFunc(p.OnProgress)
	}
	snap, err := collector.Collect(ctx, resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	scoped := filterByScope(snap.Resources, input)

	hierarchies := make(map[string]engine.TagHierarchy, len(scoped))
	resourceInfos := make(map[string]engine.ResourceInfoForSim, len(scoped))
	for _, r := range scoped {
		hierarchies[r.ID] = engine.TagHierarchy{
			ResourceTags:      r.Tags,
			ResourceGroupTags: snap.ResourceGroupTags[r.ResourceGroup],
			ResourceGroupName: r.ResourceGroup,
			SubscriptionTags:  snap.SubscriptionTags,
			SubscriptionName:  snap.SubscriptionName,
		}
		resourceInfos[r.ID] = engine.ResourceInfoForSim{Name: r.Name, Type: r.Type}
	}

	p.progress("Computing policy diff...", "")
	policyDiff := engine.SimulatePolicyChange(p.resolver, hierarchies, resourceInfos, input)

	// ── BEFORE: full pipeline with current policies ──
	p.progress("Running BEFORE scenario...", "current policies")
	beforePolicies := p.resolvePolicies(snap)
	beforeRecs := p.runRules(snap, beforePolicies)
	beforeDG := buildGraph(snap.Resources)
	beforeDE := recsToDE(beforeRecs)
	beforeSim := simulation.NewEngine(beforeDE, beforeDG).Run()
	beforeForecast := forecast.Calculate(beforeSim, forecastMonths)

	// ── AFTER: full pipeline with hypothetical policies ──
	p.progress("Running AFTER scenario...", "hypothetical policies")
	afterSnap := cloneSnapshot(snap)
	applyHypotheticalToSnap(afterSnap, input)
	afterPolicies := p.resolvePolicies(afterSnap)
	afterRecs := p.runRules(afterSnap, afterPolicies)
	afterDG := buildGraph(afterSnap.Resources)
	afterDE := recsToDE(afterRecs)
	afterSim := simulation.NewEngine(afterDE, afterDG).Run()
	afterForecast := forecast.Calculate(afterSim, forecastMonths)

	// ── DECISION DIFFS: per-resource before/after comparison ──
	p.progress("Computing impact analysis...", "")
	diffs := buildDecisionDiffs(scoped, beforeRecs, afterRecs, policyDiff, resourceInfos)

	beforeSavings := beforeSim.TotalSaving
	afterSavings := afterSim.TotalSaving
	savingsDelta := afterSavings - beforeSavings

	newlyProtected := countDecisionChange(diffs, true, false)
	newlyActionable := countDecisionChange(diffs, false, true)

	externalCount := countExternalAffected(policyDiff, afterPolicies)

	affectedPct := 0.0
	if policyDiff.TotalResources > 0 {
		affectedPct = float64(policyDiff.AffectedCount) / float64(policyDiff.TotalResources) * 100
	}
	savingsDeltaPct := 0.0
	if beforeSavings > 0 {
		savingsDeltaPct = savingsDelta / beforeSavings * 100
	}

	impact := engine.ComputeImpactScore(affectedPct, len(diffs), savingsDeltaPct, externalCount > 0)
	safety := engine.SafetyAssessment(impact, newlyProtected, newlyActionable, externalCount, savingsDelta)

	return &PolicySimResult{
		PolicySimOutput: policyDiff,

		BeforeSim:      beforeSim,
		AfterSim:       afterSim,
		BeforeForecast: beforeForecast,
		AfterForecast:  afterForecast,

		DecisionDiffs:    diffs,
		BeforeRecsCount:  len(beforeRecs),
		AfterRecsCount:   len(afterRecs),
		BeforeSavings:    beforeSavings,
		AfterSavings:     afterSavings,
		SavingsDelta:     savingsDelta,
		ExternalAffected: externalCount,

		Impact:  impact,
		Safety:  safety,
		Summary: buildSummary(policyDiff, beforeSavings, afterSavings, newlyProtected, newlyActionable),
	}, nil
}

func buildDecisionDiffs(
	resources []providers.Resource,
	beforeRecs, afterRecs []engine.Recommendation,
	policyDiff *engine.PolicySimOutput,
	infos map[string]engine.ResourceInfoForSim,
) []engine.DecisionDiff {
	beforeMap := recsToMap(beforeRecs)
	afterMap := recsToMap(afterRecs)
	changesMap := policyChangesMap(policyDiff)

	allIDs := make(map[string]bool)
	for _, r := range beforeRecs {
		allIDs[r.ResourceID] = true
	}
	for _, r := range afterRecs {
		allIDs[r.ResourceID] = true
	}

	var diffs []engine.DecisionDiff
	for id := range allIDs {
		before := beforeMap[id]
		after := afterMap[id]

		if before == nil && after == nil {
			continue
		}
		if before != nil && after != nil && !decisionChanged(before, after) {
			continue
		}

		info := infos[id]
		changes := changesMap[id]
		diff := engine.BuildDecisionDiff(id, info.Name, info.Type, before, after, changes)
		diffs = append(diffs, diff)
	}
	return diffs
}

func decisionChanged(a, b *engine.Recommendation) bool {
	if a.Action != b.Action {
		return true
	}
	if a.Risk != b.Risk {
		return true
	}
	if a.AutoExecute != b.AutoExecute {
		return true
	}
	if a.Analysis != nil && b.Analysis != nil && a.Analysis.Confidence != b.Analysis.Confidence {
		return true
	}
	return false
}

func recsToMap(recs []engine.Recommendation) map[string]*engine.Recommendation {
	m := make(map[string]*engine.Recommendation, len(recs))
	for i := range recs {
		m[recs[i].ResourceID] = &recs[i]
	}
	return m
}

func policyChangesMap(diff *engine.PolicySimOutput) map[string][]engine.FieldChange {
	m := make(map[string][]engine.FieldChange, len(diff.Comparisons))
	for _, c := range diff.Comparisons {
		m[c.ResourceID] = c.Changes
	}
	return m
}

func countDecisionChange(diffs []engine.DecisionDiff, beforeAuto, afterAuto bool) int {
	n := 0
	for _, d := range diffs {
		if d.BeforeAuto == beforeAuto && d.AfterAuto == afterAuto {
			n++
		}
	}
	return n
}

func buildGraph(resources []providers.Resource) *graph.DependencyGraph {
	dg := graph.NewDependencyGraph()
	for _, r := range resources {
		dg.AddNode(&graph.Node{ID: r.ID, Type: r.Type, Name: r.Name})
	}
	graph.LinkAzureResources(dg, resources)
	return dg
}

func recsToDE(recs []engine.Recommendation) *engine.DecisionEngine {
	de := engine.NewDecisionEngine()
	for _, r := range recs {
		de.AddRecommendation(r)
	}
	return de
}

// runRules executes rules against a snapshot with the given policies,
// including full safety context (graph, locks, snapshots, pricing).
func (p *Pipeline) runRules(snap *discovery.Snapshot, policies map[string]*engine.ResolvedPolicy) []engine.Recommendation {
	var actionable []providers.Resource
	for _, r := range snap.Resources {
		pol := policies[r.ID]
		if pol != nil && (pol.Mode == engine.ModeIgnore || pol.Mode == engine.ModeObserve) {
			continue
		}
		actionable = append(actionable, r)
	}

	analyses := p.analyzeMetrics(snap.Metrics, actionable, policies)

	dg := buildGraph(snap.Resources)

	de := engine.NewDecisionEngine()
	evalCtx := rules.EvalContext{
		Resources: actionable,
		Metrics:   snap.Metrics,
		Analyses:  analyses,
		Policies:  policies,
		Graph:     dg,
		Locks:     snap.Locks,
		Snapshots: snap.Snapshots,
		Pricing:   p.pricing,
	}
	for _, rule := range p.rules {
		for _, rec := range rule.Evaluate(evalCtx) {
			de.AddRecommendation(rec)
		}
	}

	return de.Recommendations()
}

func filterByScope(resources []providers.Resource, input engine.PolicySimInput) []providers.Resource {
	if input.Scope == engine.ScopeSubscription {
		return resources
	}
	var filtered []providers.Resource
	for _, r := range resources {
		if r.ResourceGroup == input.ScopeName {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func cloneSnapshot(snap *discovery.Snapshot) *discovery.Snapshot {
	return &discovery.Snapshot{
		Resources:         snap.Resources,
		Metrics:           snap.Metrics,
		SubscriptionTags:  copySubTags(snap.SubscriptionTags),
		SubscriptionName:  snap.SubscriptionName,
		ResourceGroupTags: copyRGTags(snap.ResourceGroupTags),
		Locks:             snap.Locks,
		Snapshots:         snap.Snapshots,
	}
}

func applyHypotheticalToSnap(snap *discovery.Snapshot, input engine.PolicySimInput) {
	expanded := input.ExpandTags()
	switch input.Scope {
	case engine.ScopeResourceGroup:
		if snap.ResourceGroupTags[input.ScopeName] == nil {
			snap.ResourceGroupTags[input.ScopeName] = make(map[string]string)
		}
		for k, v := range expanded {
			snap.ResourceGroupTags[input.ScopeName][k] = v
		}
	case engine.ScopeSubscription:
		if snap.SubscriptionTags == nil {
			snap.SubscriptionTags = make(map[string]string)
		}
		for k, v := range expanded {
			snap.SubscriptionTags[k] = v
		}
	}
}

func copyRGTags(src map[string]map[string]string) map[string]map[string]string {
	dst := make(map[string]map[string]string, len(src))
	for rg, tags := range src {
		cp := make(map[string]string, len(tags))
		for k, v := range tags {
			cp[k] = v
		}
		dst[rg] = cp
	}
	return dst
}

func copySubTags(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	cp := make(map[string]string, len(src))
	for k, v := range src {
		cp[k] = v
	}
	return cp
}

// countExternalAffected counts how many resources in the policy diff
// have ExternalDeps=true in their resolved (after) policy.
func countExternalAffected(diff *engine.PolicySimOutput, afterPolicies map[string]*engine.ResolvedPolicy) int {
	count := 0
	for _, c := range diff.Comparisons {
		if pol := afterPolicies[c.ResourceID]; pol != nil && pol.ExternalDeps {
			count++
		}
	}
	return count
}

func buildSummary(diff *engine.PolicySimOutput, beforeSavings, afterSavings float64, newlyProtected, newlyActionable int) string {
	delta := afterSavings - beforeSavings
	switch {
	case delta < 0 && newlyProtected > 0:
		return fmt.Sprintf("Applying this policy increases safety but reduces aggressive cost optimization by $%.2f/mo.", -delta)
	case delta > 0 && newlyActionable > 0:
		return fmt.Sprintf("Applying this policy enables $%.2f/mo in additional optimization by relaxing protections.", delta)
	case diff.AffectedCount == 0:
		return "No resources would be affected by this policy change."
	default:
		return fmt.Sprintf("Policy change affects %d resources with a savings delta of $%.2f/mo.", diff.AffectedCount, delta)
	}
}
