package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/engine"
)

// IdleSQLDatabaseRule detects SQL databases on paid tiers that are paused
// or potentially overprovisioned without significant activity.
type IdleSQLDatabaseRule struct{}

func (r *IdleSQLDatabaseRule) Name() string {
	return "idle-sql-database"
}

func (r *IdleSQLDatabaseRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Sql/servers/databases") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		tier := extractSKUTier(res.Properties)
		if isFreeSQLTier(tier) {
			continue
		}

		status, _ := res.Properties["status"].(string)

		var rec *engine.Recommendation

		if strings.EqualFold(status, "Paused") {
			// The old code estimated savings as 30% of the plan
			// cost. That's a guess dressed up as a number; if the
			// user resizes the DB the actual saving is completely
			// different. Emit savings=0 + explanatory note so the
			// rec stays visible but contributes nothing misleading
			// to TotalSaving.
			rec = &engine.Recommendation{
				ResourceID:   res.ID,
				ResourceType: res.Type,
				Action:       "review",
				Reason: fmt.Sprintf(
					"SQL Database paused but still on %s tier — storage costs accumulating",
					tier),
				Risk:        engine.RiskLow,
				MonthlySave: 0,
				AutoExecute: false,
				PolicyNote:  "savings depend on target tier/size — review manually to quantify",
			}
		} else if strings.EqualFold(status, "Online") && isHighSQLTier(tier) {
			rec = &engine.Recommendation{
				ResourceID:   res.ID,
				ResourceType: res.Type,
				Action:       "review",
				Reason: fmt.Sprintf(
					"SQL Database on %s tier — verify workload justifies this tier",
					tier),
				Risk:        engine.RiskMedium,
				MonthlySave: 0,
				AutoExecute: false,
				PolicyNote:  "savings depend on target tier/size — review manually to quantify",
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

func isFreeSQLTier(tier string) bool {
	t := strings.ToLower(tier)
	return t == "" || t == "free"
}

func isHighSQLTier(tier string) bool {
	t := strings.ToLower(tier)
	return t == "premium" || t == "businesscritical"
}
