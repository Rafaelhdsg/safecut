package rules

import (
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// OrphanDiskRule detects managed disks that are not attached to any VM.
type OrphanDiskRule struct{}

func (r *OrphanDiskRule) Name() string {
	return "orphan-disk"
}

// Evaluate checks a list of disk resources and flags those without an attached VM.
func (r *OrphanDiskRule) Evaluate(disks []providers.Resource) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, d := range disks {
		attached, _ := d.Properties["diskState"].(string)
		if attached == "Unattached" {
			recs = append(recs, engine.Recommendation{
				ResourceID:   d.ID,
				ResourceType: "Microsoft.Compute/disks",
				Action:       "delete",
				Reason:       "Disk is not attached to any VM and is generating idle cost",
				Risk:         engine.RiskLow,
				MonthlySave:  d.MonthlyCost,
			})
		}
	}
	return recs
}
