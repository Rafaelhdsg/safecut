package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// OrphanDiskRule detects managed disks that are not attached to any VM.
type OrphanDiskRule struct{}

func (r *OrphanDiskRule) Name() string {
	return "orphan-disk"
}

func (r *OrphanDiskRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, d := range ctx.Resources {
		if !isType(d, "Microsoft.Compute/disks") {
			continue
		}

		policy := ctx.Policies[d.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		// diskState comparison is case-insensitive: Azure's ARM API
		// normalises to "Unattached" but Resource Graph has been
		// observed returning "unattached" for disks created via the
		// CLI, and the canonical-case check silently skipped them —
		// producing "missed savings" (the opposite of false idle, but
		// equally confidence-eroding).
		attached, _ := d.Properties["diskState"].(string)
		if !strings.EqualFold(attached, "Unattached") {
			continue
		}

		// Price fallback path: if Retail API couldn't resolve the
		// disk, MonthlyCost is 0 (PriceFallback=true) and we skip
		// emitting savings — a fabricated "$1.54 minimum" was the
		// kind of guess we agreed to remove for v1.0. The rec still
		// reports the orphan so the user can see it and act.
		monthlySave := d.MonthlyCost
		priceFallback := false
		if monthlySave <= 0 {
			monthlySave = 0
			priceFallback = true
		}

		rec := engine.Recommendation{
			ResourceID:   d.ID,
			ResourceType: "Microsoft.Compute/disks",
			Action:       "delete",
			Reason:       "Disk is not attached to any VM and is generating idle cost",
			Risk:         engine.RiskLow,
			MonthlySave:  monthlySave,
			AutoExecute:  true,
		}
		if priceFallback {
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"monthly savings unavailable: Retail Prices API did not return a SKU for this disk; orphan reported with $0 savings to avoid fabricated numbers")
		}
		if a, ok := ctx.Analyses[d.ID]; ok {
			rec.Analysis = a
		}

		// Safety: graph consistency check
		if ctx.Graph != nil {
			node, ok := ctx.Graph.GetNode(d.ID)
			if ok && node.Parent != nil {
				rec.Risk = engine.RiskMedium
				rec.AutoExecute = false
				rec.PolicyNote = appendNote(rec.PolicyNote,
					"diskState=Unattached but managedBy still set — possible transient state, needs manual verification")
			}
		}

		// Safety: lock check
		if locked, lock := ctx.IsLocked(d.ID, d.ResourceGroup); locked {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				fmt.Sprintf("LOCKED (%s at %s scope) — delete would fail", lock.Level, lock.Scope))
		}

		// Safety: snapshot warning — disk may be source for active snapshots
		if ctx.HasSnapshots(d.ResourceGroup) {
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"resource group has active snapshots — verify this disk is not a snapshot source before deleting")
		}

		// Snapshot probe failed? assume a snapshot may exist.
		if ctx.SnapshotStateUnknown(d.ResourceGroup) {
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"snapshot state unknown — cannot confirm disk is not a snapshot source, manual review required")
		}

		ApplySafety(ctx, &rec, d.ID, d.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
