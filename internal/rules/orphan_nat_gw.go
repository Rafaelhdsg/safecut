package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/engine"
)

// OrphanNATGatewayRule detects NAT gateways with no subnets attached.
// A disconnected NAT gateway costs ~$32.85/mo for nothing.
type OrphanNATGatewayRule struct{}

func (r *OrphanNATGatewayRule) Name() string {
	return "orphan-nat-gateway"
}

func (r *OrphanNATGatewayRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Network/natGateways") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		subnets, _ := res.Properties["subnets"].([]interface{})
		if len(subnets) > 0 {
			continue
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "delete",
			Reason:       "NAT Gateway with no subnets attached — paying for a disconnected gateway",
			Risk:         engine.RiskLow,
			MonthlySave:  res.MonthlyCost,
			AutoExecute:  true,
		}

		// Same transient-state guard as the load balancer rule: a
		// NAT gateway mid-deploy has no subnets *yet* and must not
		// be auto-deleted based on a racy snapshot.
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

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
