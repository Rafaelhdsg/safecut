package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/engine"
)

// IdleAppServiceRule detects App Services on paid tiers that are stopped
// or otherwise wasting money without serving traffic.
type IdleAppServiceRule struct{}

func (r *IdleAppServiceRule) Name() string {
	return "idle-app-service"
}

func (r *IdleAppServiceRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Web/sites") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		tier := extractSKUTier(res.Properties)
		if isFreeAppServiceTier(tier) {
			continue
		}

		state, _ := res.Properties["state"].(string)

		var rec *engine.Recommendation

		if strings.EqualFold(state, "Stopped") {
			// Stopped App Services still incur the full plan cost,
			// so savings == MonthlyCost when we have a real number.
			// PriceFallback / MonthlyCost==0 means the rec surfaces
			// the waste but contributes $0 to TotalSaving (the
			// headline number stays honest).
			save := res.MonthlyCost
			if save < 0 {
				save = 0
			}
			rec = &engine.Recommendation{
				ResourceID:   res.ID,
				ResourceType: res.Type,
				Action:       "downgrade",
				Reason: fmt.Sprintf(
					"App Service stopped but still on %s tier — paying for idle compute",
					tier),
				Risk:        engine.RiskLow,
				MonthlySave: save,
				AutoExecute: true,
			}
			if res.MonthlyCost <= 0 {
				rec.PolicyNote = appendNote(rec.PolicyNote,
					"savings estimate unavailable (pricing API did not return a price for this SKU) — manual verification recommended")
			}
		} else if strings.EqualFold(state, "Running") && isPremiumAppServiceTier(tier) {
			alwaysOn := extractNestedBool(res.Properties, "siteConfig", "alwaysOn")
			if !alwaysOn {
				// We don't know the *target* downgrade SKU's price,
				// so we refuse to guess a savings number. The rec
				// still appears for visibility; action is manual.
				rec = &engine.Recommendation{
					ResourceID:   res.ID,
					ResourceType: res.Type,
					Action:       "review",
					Reason: fmt.Sprintf(
						"App Service on %s tier with AlwaysOn disabled — may be idle between requests, consider downgrading",
						tier),
					Risk:        engine.RiskLow,
					MonthlySave: 0,
					AutoExecute: false,
					PolicyNote:  "downgrade savings depend on target plan — review manually to quantify",
				}
			}
		}

		if rec == nil {
			continue
		}

		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope)", lock.Level, lock.Scope))
		}

		ApplySafety(ctx, rec, res.ID, res.ResourceGroup)
		ApplyPolicy(rec, policy)
		recs = append(recs, *rec)
	}
	return recs
}

func extractSKUTier(props map[string]interface{}) string {
	// Resource Graph flattens sku into properties for some types
	if sku, ok := props["sku"].(map[string]interface{}); ok {
		if tier, ok := sku["tier"].(string); ok {
			return tier
		}
	}
	return ""
}

func isFreeAppServiceTier(tier string) bool {
	t := strings.ToLower(tier)
	return t == "" || t == "free" || t == "shared"
}

func isPremiumAppServiceTier(tier string) bool {
	t := strings.ToLower(tier)
	return t == "standard" || t == "premium" || t == "premiumv2" || t == "premiumv3"
}

func extractNestedBool(m map[string]interface{}, keys ...string) bool {
	current := m
	for i, key := range keys {
		v, ok := current[key]
		if !ok || v == nil {
			return false
		}
		if i == len(keys)-1 {
			b, ok := v.(bool)
			return ok && b
		}
		next, ok := v.(map[string]interface{})
		if !ok {
			return false
		}
		current = next
	}
	return false
}
