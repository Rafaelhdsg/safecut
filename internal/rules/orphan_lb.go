package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// OrphanLoadBalancerRule detects Standard SKU load balancers with no
// backends configured — paying for an idle balancer.
type OrphanLoadBalancerRule struct{}

func (r *OrphanLoadBalancerRule) Name() string {
	return "orphan-load-balancer"
}

func (r *OrphanLoadBalancerRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Network/loadBalancers") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		skuName := extractSKUName(res.Properties)
		if skuName == "" || skuName == "Basic" {
			continue
		}

		if hasBackendConfigurations(res.Properties) {
			continue
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "delete",
			Reason:       "Standard Load Balancer with no backend pools configured — paying for an unused balancer",
			Risk:         engine.RiskLow,
			MonthlySave:  res.MonthlyCost,
			AutoExecute:  true,
		}

		// A balancer whose provisioningState is still Updating,
		// Creating, Deleting or Failed is NOT safe to auto-delete
		// on the strength of a one-shot "no backends" reading. We
		// surface the rec for visibility but downgrade it so nothing
		// runs until the resource reaches Succeeded.
		if ps := extractProvisioningState(res.Properties); ps != "" && !strings.EqualFold(ps, "Succeeded") {
			rec.AutoExecute = false
			rec.Risk = engine.BumpRisk(rec.Risk, 1)
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("resource in transient state %q — manual review required", ps))
		}

		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope)", lock.Level, lock.Scope))
		}

		if ctx.Graph != nil && ctx.Graph.HasDependents(res.ID) {
			rec.Risk = engine.RiskMedium
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"Has dependents in resource graph — verify before deletion")
		}

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}

func extractSKUName(props map[string]interface{}) string {
	if sku, ok := props["sku"].(map[string]interface{}); ok {
		if name, ok := sku["name"].(string); ok {
			return name
		}
	}
	return ""
}

func hasBackendConfigurations(props map[string]interface{}) bool {
	pools, ok := props["backendAddressPools"].([]interface{})
	if !ok || len(pools) == 0 {
		return false
	}
	for _, poolRaw := range pools {
		pool, ok := poolRaw.(map[string]interface{})
		if !ok {
			continue
		}
		poolProps, _ := pool["properties"].(map[string]interface{})
		if poolProps == nil {
			poolProps = pool
		}
		if configs, ok := poolProps["backendIPConfigurations"].([]interface{}); ok && len(configs) > 0 {
			return true
		}
	}
	return false
}
