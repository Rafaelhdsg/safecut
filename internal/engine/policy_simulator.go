package engine

import "fmt"

// SimScope defines at which level the simulated policy change is applied.
type SimScope string

const (
	ScopeResourceGroup SimScope = "resource_group"
	ScopeSubscription  SimScope = "subscription"
)

// PolicySimInput describes the hypothetical policy change to simulate.
type PolicySimInput struct {
	Scope     SimScope
	ScopeName string
	SetTags   map[string]string // shorthand keys: "criticality", "mode", "external", "template"
}

// ExpandTags converts shorthand --set keys into full InfraMind tag names.
func (i PolicySimInput) ExpandTags() map[string]string {
	expanded := make(map[string]string, len(i.SetTags))
	for k, v := range i.SetTags {
		switch k {
		case "criticality":
			expanded["inframind-criticality"] = v
		case "mode":
			expanded["inframind-mode"] = v
		case "external":
			expanded["inframind-external"] = v
		case "template":
			expanded["inframind-template"] = v
		default:
			expanded[k] = v
		}
	}
	return expanded
}

// FieldChange records a single field that changed between before and after.
type FieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
	Impact string `json:"impact"`
}

// PolicyComparison is the before/after diff for a single resource.
type PolicyComparison struct {
	ResourceID   string          `json:"resource_id"`
	ResourceName string          `json:"resource_name"`
	ResourceType string          `json:"resource_type"`
	Before       *ResolvedPolicy `json:"before"`
	After        *ResolvedPolicy `json:"after"`
	Changes      []FieldChange   `json:"changes"`
	Inherited    bool            `json:"inherited"`
}

// ImpactLevel classifies the overall blast radius of a policy change.
type ImpactLevel string

const (
	ImpactLow      ImpactLevel = "LOW"
	ImpactMedium   ImpactLevel = "MEDIUM"
	ImpactHigh     ImpactLevel = "HIGH"
	ImpactCritical ImpactLevel = "CRITICAL"
)

// DecisionDiff is the full before/after comparison of a recommendation
// for a single resource — the core of explainability in policy simulation.
type DecisionDiff struct {
	ResourceID   string    `json:"resource_id"`
	ResourceName string    `json:"resource_name"`
	ResourceType string    `json:"resource_type"`
	BeforeAction string    `json:"before_action"`
	AfterAction  string    `json:"after_action"`
	BeforeRisk   RiskLevel `json:"before_risk"`
	AfterRisk    RiskLevel `json:"after_risk"`
	BeforeAuto   bool      `json:"before_auto"`
	AfterAuto    bool      `json:"after_auto"`
	BeforeConf   float64   `json:"before_confidence"`
	AfterConf    float64   `json:"after_confidence"`
	BeforeSaving float64   `json:"before_saving"`
	AfterSaving  float64   `json:"after_saving"`
	Explanation  []string  `json:"explanation"`
}

// PolicySimOutput is the result of comparing policies before and after a change.
type PolicySimOutput struct {
	Scope          SimScope           `json:"scope"`
	ScopeName      string             `json:"scope_name"`
	SetTags        map[string]string  `json:"set_tags"`
	TotalResources int                `json:"total_resources"`
	AffectedCount  int                `json:"affected_count"`
	InheritedCount int                `json:"inherited_count"`
	Comparisons    []PolicyComparison `json:"comparisons"`
	DriftsResolved int                `json:"drifts_resolved"`
	DriftsCreated  int                `json:"drifts_created"`
}

// ComputeImpactScore evaluates the blast radius of a policy change
// across three dimensions: scope, decisions, and financial impact.
// If any affected resource has ExternalDeps=true, the score is
// automatically escalated to CRITICAL — touching unknown external
// dependencies is the highest operational risk.
func ComputeImpactScore(affectedPct float64, decisionChanges int, savingsDeltaPct float64, externalAffected bool) ImpactLevel {
	if externalAffected {
		return ImpactCritical
	}

	score := 0

	switch {
	case affectedPct > 50:
		score += 3
	case affectedPct > 20:
		score += 2
	case affectedPct > 5:
		score += 1
	}

	switch {
	case decisionChanges > 10:
		score += 3
	case decisionChanges > 3:
		score += 2
	case decisionChanges > 0:
		score += 1
	}

	absDelta := savingsDeltaPct
	if absDelta < 0 {
		absDelta = -absDelta
	}
	switch {
	case absDelta > 30:
		score += 3
	case absDelta > 10:
		score += 2
	case absDelta > 0:
		score += 1
	}

	switch {
	case score >= 7:
		return ImpactCritical
	case score >= 5:
		return ImpactHigh
	case score >= 3:
		return ImpactMedium
	default:
		return ImpactLow
	}
}

// BuildDecisionDiff compares before/after recommendations for a single resource
// and generates a human-readable explanation chain.
func BuildDecisionDiff(resourceID, resourceName, resourceType string, before, after *Recommendation, policyChanges []FieldChange) DecisionDiff {
	diff := DecisionDiff{
		ResourceID:   resourceID,
		ResourceName: resourceName,
		ResourceType: resourceType,
	}

	if before != nil {
		diff.BeforeAction = before.Action
		diff.BeforeRisk = before.Risk
		diff.BeforeAuto = before.AutoExecute
		diff.BeforeSaving = before.MonthlySave
		if before.Analysis != nil {
			diff.BeforeConf = before.Analysis.Confidence
		}
	} else {
		diff.BeforeAction = "(none)"
	}

	if after != nil {
		diff.AfterAction = after.Action
		diff.AfterRisk = after.Risk
		diff.AfterAuto = after.AutoExecute
		diff.AfterSaving = after.MonthlySave
		if after.Analysis != nil {
			diff.AfterConf = after.Analysis.Confidence
		}
	} else {
		diff.AfterAction = "(none)"
	}

	diff.Explanation = buildExplanation(before, after, policyChanges)
	return diff
}

func buildExplanation(before, after *Recommendation, changes []FieldChange) []string {
	var reasons []string

	for _, ch := range changes {
		reasons = append(reasons, fmt.Sprintf("%s changed from %s to %s", ch.Field, displayVal(ch.Before), displayVal(ch.After)))
	}

	if before != nil && after != nil {
		if before.Risk != after.Risk {
			reasons = append(reasons, fmt.Sprintf("risk level changed: %s → %s", before.Risk, after.Risk))
		}
		if before.AutoExecute && !after.AutoExecute {
			reasons = append(reasons, "auto-execution disabled")
		} else if !before.AutoExecute && after.AutoExecute {
			reasons = append(reasons, "auto-execution enabled")
		}
		if before.Analysis != nil && after.Analysis != nil && before.Analysis.Confidence != after.Analysis.Confidence {
			reasons = append(reasons, fmt.Sprintf("confidence changed: %.2f → %.2f", before.Analysis.Confidence, after.Analysis.Confidence))
		}
	}

	if before != nil && after == nil {
		reasons = append(reasons, "recommendation removed (resource excluded or no longer idle)")
	}
	if before == nil && after != nil {
		reasons = append(reasons, "new recommendation generated")
	}

	return reasons
}

func displayVal(v string) string {
	if v == "" {
		return "(default)"
	}
	return v
}

// SafetyAssessment generates a final human-readable recommendation.
func SafetyAssessment(impact ImpactLevel, newlyProtected, newlyActionable, externalCount int, savingsDelta float64) string {
	if externalCount > 0 {
		return fmt.Sprintf(
			"CRITICAL: %d affected resource(s) have external dependencies. "+
				"These resources may be consumed by systems outside the cloud graph (VPN, ExpressRoute, third-party). "+
				"Manually verify external consumers before applying this policy change.",
			externalCount)
	}

	switch {
	case impact == ImpactLow && newlyProtected == 0 && newlyActionable == 0:
		return "This change is SAFE. Minimal impact on existing recommendations."
	case impact == ImpactLow && newlyProtected > 0:
		return "This change is SAFE and increases protection. Recommended for production."
	case impact == ImpactMedium && newlyProtected > 0 && newlyActionable == 0:
		return "This change is SAFE for production but will reduce optimization aggressiveness."
	case impact == ImpactMedium && newlyActionable > 0:
		return "CAUTION: This change relaxes protections. Review affected resources before applying."
	case impact == ImpactHigh && newlyProtected > 0:
		return "SIGNIFICANT CHANGE: Increases safety substantially but reduces cost optimization. Review the savings impact."
	case impact == ImpactHigh && newlyActionable > 0:
		return "WARNING: This change significantly relaxes protections. Ensure all affected resources are safe for automation."
	case impact == ImpactCritical:
		return "CRITICAL CHANGE: Major blast radius. Strongly recommend applying to a test environment first."
	default:
		return "Review the impact summary before applying this policy change."
	}
}

// SimulatePolicyChange compares current policies against hypothetical
// policies where the given tags are applied at the specified scope.
func SimulatePolicyChange(
	resolver *PolicyResolver,
	hierarchies map[string]TagHierarchy,
	resources map[string]ResourceInfoForSim,
	input PolicySimInput,
) *PolicySimOutput {
	expandedTags := input.ExpandTags()

	output := &PolicySimOutput{
		Scope:          input.Scope,
		ScopeName:      input.ScopeName,
		SetTags:        input.SetTags,
		TotalResources: len(hierarchies),
	}

	for id, h := range hierarchies {
		before := resolver.Resolve(h)

		after := resolver.Resolve(applyHypothetical(h, input.Scope, expandedTags))

		changes := diffPolicies(before, after)
		if len(changes) == 0 {
			continue
		}

		info := resources[id]
		inherited := !hasExplicitOverlap(h.ResourceTags, expandedTags)

		output.Comparisons = append(output.Comparisons, PolicyComparison{
			ResourceID:   id,
			ResourceName: info.Name,
			ResourceType: info.Type,
			Before:       before,
			After:        after,
			Changes:      changes,
			Inherited:    inherited,
		})

		output.AffectedCount++
		if inherited {
			output.InheritedCount++
		}

		output.DriftsResolved += countResolvedDrifts(before.Drifts, after.Drifts)
		output.DriftsCreated += countNewDrifts(before.Drifts, after.Drifts)
	}

	return output
}

// ResourceInfoForSim holds minimal resource metadata for simulation display.
type ResourceInfoForSim struct {
	Name string
	Type string
}

func applyHypothetical(h TagHierarchy, scope SimScope, tags map[string]string) TagHierarchy {
	hyp := TagHierarchy{
		ResourceTags:      copyTags(h.ResourceTags),
		ResourceGroupTags: copyTags(h.ResourceGroupTags),
		ResourceGroupName: h.ResourceGroupName,
		SubscriptionTags:  copyTags(h.SubscriptionTags),
		SubscriptionName:  h.SubscriptionName,
	}

	switch scope {
	case ScopeResourceGroup:
		if hyp.ResourceGroupTags == nil {
			hyp.ResourceGroupTags = make(map[string]string)
		}
		for k, v := range tags {
			hyp.ResourceGroupTags[k] = v
		}
	case ScopeSubscription:
		if hyp.SubscriptionTags == nil {
			hyp.SubscriptionTags = make(map[string]string)
		}
		for k, v := range tags {
			hyp.SubscriptionTags[k] = v
		}
	}

	return hyp
}

func copyTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	cp := make(map[string]string, len(tags))
	for k, v := range tags {
		cp[k] = v
	}
	return cp
}

func diffPolicies(before, after *ResolvedPolicy) []FieldChange {
	var changes []FieldChange

	if before.Mode != after.Mode {
		changes = append(changes, FieldChange{
			Field:  "mode",
			Before: string(before.Mode),
			After:  string(after.Mode),
			Impact: describeImpact("mode", string(before.Mode), string(after.Mode)),
		})
	}
	if before.Criticality != after.Criticality {
		changes = append(changes, FieldChange{
			Field:  "criticality",
			Before: string(before.Criticality),
			After:  string(after.Criticality),
			Impact: describeImpact("criticality", string(before.Criticality), string(after.Criticality)),
		})
	}
	if before.ExternalDeps != after.ExternalDeps {
		changes = append(changes, FieldChange{
			Field:  "external",
			Before: fmt.Sprintf("%v", before.ExternalDeps),
			After:  fmt.Sprintf("%v", after.ExternalDeps),
			Impact: describeImpact("external", fmt.Sprintf("%v", before.ExternalDeps), fmt.Sprintf("%v", after.ExternalDeps)),
		})
	}

	return changes
}

func describeImpact(field, before, after string) string {
	switch field {
	case "mode":
		return describeModeImpact(PolicyMode(before), PolicyMode(after))
	case "criticality":
		return describeCriticalityImpact(Criticality(before), Criticality(after))
	case "external":
		if after == "true" {
			return "Confidence will be halved, manual review required"
		}
		return "Confidence penalty removed, auto-execution may be allowed"
	}
	return ""
}

func describeModeImpact(before, after PolicyMode) string {
	switch {
	case after == ModeIgnore:
		return "Resource will be completely excluded from analysis"
	case after == ModeObserve:
		return "Resource will be analyzed but no recommendations generated"
	case after == ModeProtect:
		return "Recommendations will require manual approval"
	case before == ModeProtect && (after == ModeDefault || after == ""):
		return "Auto-execution will be allowed"
	case before == ModeIgnore && after != ModeIgnore:
		return "Resource will now be included in analysis"
	default:
		return ""
	}
}

func describeCriticalityImpact(before, after Criticality) string {
	switch {
	case after == CriticalityHigh:
		return "Thresholds will tighten (×0.5), risk +1, auto-execution blocked"
	case after == CriticalityLow:
		return "Thresholds will be more aggressive (×2.0), easier to flag idle"
	case after == CriticalityMedium:
		return "Standard thresholds applied"
	case after == CriticalityNone && before == CriticalityHigh:
		return "Protection removed — thresholds return to standard, auto-execution allowed"
	default:
		return ""
	}
}

func hasExplicitOverlap(resourceTags, setTags map[string]string) bool {
	for k := range setTags {
		if _, ok := resourceTags[k]; ok {
			return true
		}
	}
	return false
}

func countResolvedDrifts(before, after []PolicyDrift) int {
	resolved := 0
	for _, b := range before {
		found := false
		for _, a := range after {
			if b.Field == a.Field && b.ParentName == a.ParentName {
				found = true
				break
			}
		}
		if !found {
			resolved++
		}
	}
	return resolved
}

func countNewDrifts(before, after []PolicyDrift) int {
	created := 0
	for _, a := range after {
		found := false
		for _, b := range before {
			if a.Field == b.Field && a.ParentName == b.ParentName {
				found = true
				break
			}
		}
		if !found {
			created++
		}
	}
	return created
}
