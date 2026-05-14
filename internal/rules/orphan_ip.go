package rules

import (
	"fmt"

	"github.com/Rafaelhdsg/safecut/internal/engine"
)

// OrphanIPRule detects public IP addresses that are not associated to any resource.
type OrphanIPRule struct{}

func (r *OrphanIPRule) Name() string {
	return "orphan-ip"
}

func (r *OrphanIPRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, ip := range ctx.Resources {
		if !isType(ip, "Microsoft.Network/publicIPAddresses") {
			continue
		}

		policy := ctx.Policies[ip.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		hasIPConfig := ip.Properties["ipConfiguration"] != nil

		// Double-check: even if properties say no ipConfiguration, the graph
		// may have linked this IP to a NIC via cached data. Trust both sources.
		graphLinked := false
		if ctx.Graph != nil {
			node, ok := ctx.Graph.GetNode(ip.ID)
			if ok && node.Parent != nil {
				graphLinked = true
			}
		}

		if hasIPConfig || graphLinked {
			continue
		}

		rec := engine.Recommendation{
			ResourceID:   ip.ID,
			ResourceType: ip.Type,
			Action:       "deallocate",
			Reason:       "Public IP is not associated to any resource — idle cost with no value",
			Risk:         engine.RiskLow,
			MonthlySave:  ip.MonthlyCost,
			AutoExecute:  true,
		}
		if a, ok := ctx.Analyses[ip.ID]; ok {
			rec.Analysis = a
		}

		// Safety: lock check
		if locked, lock := ctx.IsLocked(ip.ID, ip.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope) — deallocate would fail", lock.Level, lock.Scope))
		}

		ApplySafety(ctx, &rec, ip.ID, ip.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
