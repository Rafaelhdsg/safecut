package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/engine"
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

		// Dedicated rules already own these types. Running this rule
		// on them produces a second recommendation for the same
		// resource and inflates TotalSaving via double counting.
		// OrphanDiskRule owns disks; OrphanIPRule owns public IPs.
		lowerType := strings.ToLower(res.Type)
		if strings.Contains(lowerType, "microsoft.compute/disks") ||
			strings.Contains(lowerType, "microsoft.network/publicipaddresses") {
			continue
		}

		// Skip VMs that are already deallocated — no compute cost to save.
		// An empty PowerState means the provider couldn't determine
		// the VM's state; we *don't* skip it, but we downgrade the
		// rec at the end so auto-execute is impossible.
		isVM := isType(res, "Microsoft.Compute/virtualMachines")
		if isVM && (res.PowerState == "deallocated" || res.PowerState == "stopped") {
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

		monthlySave := res.MonthlyCost
		if monthlySave <= 0 {
			monthlySave = estimateMinCost(res.Type)
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
			MonthlySave: monthlySave,
			Analysis:    analysis,
			AutoExecute: true,
		}

		// Safety: dependency check
		if ctx.Graph != nil && ctx.Graph.HasDependents(res.ID) {
			rec.Risk = engine.RiskMedium
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"has active dependents — manual review required before action")
		}

		// Safety: lock check
		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope) — action would fail", lock.Level, lock.Scope))
		}

		// Unknown power state: refuse to auto-execute. Seen on
		// Resource Graph responses where instanceView was trimmed
		// or the caller lacks Reader at the VM scope — we can't tell
		// if the VM is running, deallocated, or mid-transition.
		if isVM && strings.TrimSpace(res.PowerState) == "" {
			rec.AutoExecute = false
			rec.Risk = engine.BumpRisk(rec.Risk, 1)
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"power state unknown — cannot verify safely auto-stop, manual review required")
		}

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}

func recommendAction(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch t {
	case "microsoft.compute/virtualmachines":
		return "stop"
	case "microsoft.compute/disks":
		return "delete"
	case "microsoft.network/publicipaddresses":
		return "deallocate"
	case "microsoft.web/sites":
		return "downgrade"
	case "microsoft.sql/servers/databases":
		return "review"
	case "microsoft.network/loadbalancers":
		return "delete"
	case "microsoft.network/natgateways":
		return "delete"
	case "microsoft.containerinstance/containergroups":
		return "delete"
	default:
		return "review"
	}
}

func estimateMinCost(resourceType string) float64 {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "virtualmachines"):
		return 7.59
	case strings.Contains(t, "disks"):
		return 1.54
	case strings.Contains(t, "publicipaddresses"):
		return 3.65
	default:
		return 0
	}
}
