package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/engine"
)

// vmDownsizeMap encodes the "next smaller SKU in the same family"
// graph. Prices are NOT stored here — every monetary figure comes
// from the live Retail Prices API so a rightsize rec never shows a
// stale-by-months number. The tuple is (vCPUs, downsizeTo). vCPUs
// stays because we need it for the human-readable reason only;
// nothing else reads it.
type vmDownsizeInfo struct {
	vCPUs      int
	downsizeTo string
}

var vmDownsizeMap = map[string]vmDownsizeInfo{
	"standard_b1s":  {1, ""},
	"standard_b1ms": {1, "standard_b1s"},
	"standard_b2s":  {2, "standard_b1ms"},
	"standard_b2ms": {2, "standard_b2s"},
	"standard_b4ms": {4, "standard_b2ms"},
	"standard_b8ms": {8, "standard_b4ms"},

	"standard_d2s_v3":  {2, ""},
	"standard_d4s_v3":  {4, "standard_d2s_v3"},
	"standard_d8s_v3":  {8, "standard_d4s_v3"},
	"standard_d16s_v3": {16, "standard_d8s_v3"},
	"standard_d32s_v3": {32, "standard_d16s_v3"},

	"standard_d2s_v5":  {2, ""},
	"standard_d4s_v5":  {4, "standard_d2s_v5"},
	"standard_d8s_v5":  {8, "standard_d4s_v5"},
	"standard_d16s_v5": {16, "standard_d8s_v5"},
	"standard_d32s_v5": {32, "standard_d16s_v5"},

	"standard_e2s_v3":  {2, ""},
	"standard_e4s_v3":  {4, "standard_e2s_v3"},
	"standard_e8s_v3":  {8, "standard_e4s_v3"},
	"standard_e16s_v3": {16, "standard_e8s_v3"},
	"standard_e2s_v5":  {2, ""},
	"standard_e4s_v5":  {4, "standard_e2s_v5"},
	"standard_e8s_v5":  {8, "standard_e4s_v5"},

	"standard_f2s_v2":  {2, ""},
	"standard_f4s_v2":  {4, "standard_f2s_v2"},
	"standard_f8s_v2":  {8, "standard_f4s_v2"},
	"standard_f16s_v2": {16, "standard_f8s_v2"},

	"standard_a1_v2": {1, ""},
	"standard_a2_v2": {2, "standard_a1_v2"},
	"standard_a4_v2": {4, "standard_a2_v2"},
}

// RightsizeRule detects VMs that are oversized: running but consistently
// using a small fraction of their allocated CPU. Recommends downsizing
// to the next smaller size in the same family.
type RightsizeRule struct {
	MaxCPUPercent float64 // CPU avg below this = oversized (default 30%)
	MinConfidence float64
}

func DefaultRightsizeRule() *RightsizeRule {
	return &RightsizeRule{
		MaxCPUPercent: 30.0,
		MinConfidence: 0.60,
	}
}

func (r *RightsizeRule) Name() string {
	return "rightsize-vm"
}

func (r *RightsizeRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Compute/virtualMachines") {
			continue
		}

		// Only rightsize running VMs — deallocated VMs have no compute cost
		if res.PowerState == "deallocated" || res.PowerState == "stopped" {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		analysis, ok := ctx.Analyses[res.ID]
		if !ok || analysis == nil {
			continue
		}

		// Skip fully idle VMs — the IdleResourceRule handles those
		if analysis.Score >= 0.85 {
			continue
		}

		if analysis.Confidence < r.MinConfidence {
			continue
		}

		cpuAvg := getCPUFromAnalysis(analysis)
		if cpuAvg <= 0 || cpuAvg >= r.MaxCPUPercent {
			continue
		}

		currentSize := extractVMSize(res.Properties)
		if currentSize == "" {
			continue
		}

		current, ok := vmDownsizeMap[currentSize]
		if !ok {
			continue
		}
		if current.downsizeTo == "" {
			continue
		}

		if _, ok := vmDownsizeMap[current.downsizeTo]; !ok {
			continue
		}

		// Fail-closed on pricing. If the Retail API couldn't give us
		// a real number for either SKU, we refuse to emit a savings
		// figure — a guessed number next to "auto-rightsize safe" is
		// exactly the kind of wrong-number-in-the-output that burns
		// user trust on launch.
		currentCost, targetCost, priceErr := resolveRightsizeCosts(ctx, currentSize, current.downsizeTo, res.Location)
		if priceErr != nil {
			ctx.RecordPricingWarning(fmt.Sprintf(
				"rightsize %s (%s): %v — skipped",
				strings.ToUpper(currentSize), res.Location, priceErr,
			))
			continue
		}
		savings := currentCost - targetCost
		if savings <= 0 {
			continue
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "rightsize",
			Reason: fmt.Sprintf(
				"VM oversized: CPU avg %.1f%% (14d) on %d vCPUs. Downsize %s → %s",
				cpuAvg, current.vCPUs,
				strings.ToUpper(currentSize), strings.ToUpper(current.downsizeTo),
			),
			Risk:        engine.RiskLow,
			MonthlySave: savings,
			Analysis:    analysis,
			AutoExecute: false,
		}

		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope) — resize would fail", lock.Level, lock.Scope))
		}

		if ctx.Graph != nil && ctx.Graph.HasDependents(res.ID) {
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"VM has dependents — resize requires brief restart")
		}

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}

func getCPUFromAnalysis(a *engine.IdleAnalysis) float64 {
	for _, sig := range a.Signals {
		if sig.Name == "cpu" {
			return sig.Value
		}
	}
	return 0
}

func extractVMSize(props map[string]interface{}) string {
	hw, ok := props["hardwareProfile"].(map[string]interface{})
	if !ok {
		return ""
	}
	size, _ := hw["vmSize"].(string)
	return strings.ToLower(size)
}

// resolveRightsizeCosts returns real pricing for current and target
// VM sizes via the Retail Prices API. If either lookup fails, or no
// PricingProvider is wired in, it returns an error and the caller
// must NOT emit a recommendation. Hardcoded price fallbacks were
// removed deliberately: a 12-month-old "typical price" next to a
// freshly-discovered VM is worse than no number at all.
func resolveRightsizeCosts(ctx EvalContext, currentSize, targetSize, location string) (float64, float64, error) {
	if ctx.Pricing == nil {
		return 0, 0, fmt.Errorf("pricing provider unavailable")
	}

	bgCtx := context.Background()
	currentPrice, err := ctx.Pricing.GetVMPrice(bgCtx, currentSize, location)
	if err != nil {
		return 0, 0, fmt.Errorf("current size %s: %w", currentSize, err)
	}
	if currentPrice <= 0 {
		return 0, 0, fmt.Errorf("current size %s: zero/negative price", currentSize)
	}

	targetPrice, err := ctx.Pricing.GetVMPrice(bgCtx, targetSize, location)
	if err != nil {
		return 0, 0, fmt.Errorf("target size %s: %w", targetSize, err)
	}
	if targetPrice <= 0 {
		return 0, 0, fmt.Errorf("target size %s: zero/negative price", targetSize)
	}
	return currentPrice, targetPrice, nil
}
