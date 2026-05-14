package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/discovery"
	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
)

// ReservedInstanceRule detects VMs that have been running consistently
// and would benefit from a Reserved Instance or Savings Plan commitment.
// A VM running 24/7 for 90+ days at steady utilization is a strong RI candidate.
type ReservedInstanceRule struct {
	MinRunDays    int     // minimum observed days to recommend RI (default 12 of 14)
	MinCPUPercent float64 // VM must show real usage — not idle (default 5%)
}

func DefaultReservedInstanceRule() *ReservedInstanceRule {
	return &ReservedInstanceRule{
		MinRunDays:    12,
		MinCPUPercent: 5.0,
	}
}

func (r *ReservedInstanceRule) Name() string {
	return "reserved-instance"
}

func (r *ReservedInstanceRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Compute/virtualMachines") {
			continue
		}

		if res.PowerState == "deallocated" || res.PowerState == "stopped" {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		metrics, ok := ctx.Metrics[res.ID]
		if !ok || metrics == nil {
			continue
		}

		// If the metrics collection layer couldn't reach Monitor, we
		// cannot claim a VM has been "running steady at X% CPU" —
		// skip, don't fabricate an RI recommendation.
		if metrics.Status == discovery.MetricsDenied {
			continue
		}

		// Must have enough observed data. We intentionally no longer
		// fall back to "assume 14 days" here; ObservedDays is the
		// real sample count surfaced by the provider.
		observedDays := metrics.ObservedDays
		if observedDays == 0 {
			continue
		}

		analysis := ctx.Analyses[res.ID]

		cpuAvg := metrics.CPUAvgPercent
		if analysis != nil {
			for _, sig := range analysis.Signals {
				if sig.Name == "cpu" && sig.Value > cpuAvg {
					cpuAvg = sig.Value
				}
			}
		}

		// Skip idle VMs — they should be stopped, not reserved
		if cpuAvg < r.MinCPUPercent {
			continue
		}

		// Skip VMs that were idle most of the period
		if analysis != nil && analysis.Score >= 0.85 {
			continue
		}

		// Check if VM was running consistently (not idle for most
		// days). Use the real ObservedDays as the denominator — a VM
		// with only 5 days of Monitor data shouldn't be treated as if
		// it had the full 14-day window.
		runningDays := observedDays - metrics.IdleDays
		if runningDays < r.MinRunDays {
			continue
		}

		monthlyCost := res.MonthlyCost
		if monthlyCost <= 0 {
			continue
		}

		// Reservation pricing is fetched from the Retail API per
		// (size, region, term). If either term is unavailable we
		// refuse to emit a savings number — launching with a
		// heuristic "36%" would make every RI rec a guess and undo
		// the whole point of v1.0 confidence.
		vmSize := extractVMSize(res.Properties)
		if vmSize == "" {
			continue
		}
		if ctx.Pricing == nil {
			ctx.RecordPricingWarning(fmt.Sprintf(
				"reservation pricing unavailable for %s in %s (no pricing provider) — rec skipped",
				strings.ToUpper(vmSize), res.Location,
			))
			continue
		}

		bg := context.Background()
		res1yr, err1 := ctx.Pricing.GetVMReservationPrice(bg, vmSize, res.Location, pricing.Reservation1Year)
		if err1 != nil || res1yr <= 0 {
			ctx.RecordPricingWarning(fmt.Sprintf(
				"reservation 1yr price for %s in %s: %v — rec skipped",
				strings.ToUpper(vmSize), res.Location, err1,
			))
			continue
		}
		saving1yr := monthlyCost - res1yr
		if saving1yr <= 0 {
			continue
		}
		// 3yr is informational only — if missing, we still emit the
		// 1yr rec but omit the 3yr figure.
		res3yr, err3 := ctx.Pricing.GetVMReservationPrice(bg, vmSize, res.Location, pricing.Reservation3Years)

		reason := fmt.Sprintf(
			"VM running steady at %.1f%% CPU for %d+ days. 1-yr RI saves $%.0f/mo",
			cpuAvg, runningDays, saving1yr,
		)
		if err3 == nil && res3yr > 0 {
			saving3yr := monthlyCost - res3yr
			if saving3yr > 0 {
				reason = fmt.Sprintf(
					"VM running steady at %.1f%% CPU for %d+ days. 1-yr RI saves $%.0f/mo; 3-yr saves $%.0f/mo",
					cpuAvg, runningDays, saving1yr, saving3yr,
				)
			}
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "reserve",
			Reason:       reason,
			Risk:         engine.RiskLow,
			MonthlySave:  saving1yr,
			Analysis:     analysis,
			AutoExecute:  false,
		}

		rec.PolicyNote = fmt.Sprintf("Current size: %s ($%.2f/mo pay-as-you-go, RI-1yr $%.2f/mo)",
			strings.ToUpper(vmSize), monthlyCost, res1yr)

		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope) — RI commitment still valid", lock.Level, lock.Scope))
		}
		if ctx.Graph != nil && ctx.Graph.HasDependents(res.ID) {
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"VM has dependents — RI commitment covers the full VM including attached resources")
		}

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
