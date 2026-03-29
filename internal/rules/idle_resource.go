package rules

import (
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// IdleResourceRule flags resources that are idle with high confidence,
// based on correlated signals (CPU + network + disk).
type IdleResourceRule struct {
	MinIdleScore  float64
	MinConfidence float64
}

func DefaultIdleResourceRule() *IdleResourceRule {
	return &IdleResourceRule{
		MinIdleScore:  0.85,
		MinConfidence: 0.80,
	}
}

func (r *IdleResourceRule) Name() string {
	return "idle-resource"
}

func (r *IdleResourceRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		policy := ctx.Policies[res.ID]

		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		analysis, ok := ctx.Analyses[res.ID]
		if !ok || analysis == nil {
			continue
		}

		if analysis.Score < r.MinIdleScore || analysis.Confidence < r.MinConfidence {
			continue
		}

		risk := engine.RiskLow
		if analysis.Confidence < 0.90 {
			risk = engine.RiskMedium
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       recommendAction(res.Type),
			Reason: fmt.Sprintf(
				"Resource idle (score=%.2f, confidence=%.2f) — correlated low CPU, silent network, no disk writes",
				analysis.Score, analysis.Confidence,
			),
			Risk:        risk,
			MonthlySave: res.MonthlyCost,
			Analysis:    analysis,
			AutoExecute: true,
		}

		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}

func recommendAction(resourceType string) string {
	switch resourceType {
	case "Microsoft.Compute/virtualMachines":
		return "stop"
	case "Microsoft.Compute/disks":
		return "delete"
	case "Microsoft.Network/publicIPAddresses":
		return "deallocate"
	default:
		return "review"
	}
}
