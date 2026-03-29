package rules

import (
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
		if d.Type != "Microsoft.Compute/disks" {
			continue
		}

		policy := ctx.Policies[d.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		attached, _ := d.Properties["diskState"].(string)
		if attached != "Unattached" {
			continue
		}

		rec := engine.Recommendation{
			ResourceID:   d.ID,
			ResourceType: "Microsoft.Compute/disks",
			Action:       "delete",
			Reason:       "Disk is not attached to any VM and is generating idle cost",
			Risk:         engine.RiskLow,
			MonthlySave:  d.MonthlyCost,
			AutoExecute:  true,
		}
		if a, ok := ctx.Analyses[d.ID]; ok {
			rec.Analysis = a
		}

		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
