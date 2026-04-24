package rules

import (
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// IdleStorageAccountRule flags storage accounts that lack access tracking,
// suggesting a review to determine if they're still needed.
type IdleStorageAccountRule struct{}

func (r *IdleStorageAccountRule) Name() string {
	return "idle-storage-account"
}

func (r *IdleStorageAccountRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.Storage/storageAccounts") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		hasTracking := extractNestedBool(res.Properties,
			"lastAccessTimeTrackingPolicy", "enable")
		if hasTracking {
			continue
		}

		reason := "Storage account has no last-access-time tracking — unable to determine if actively used. Enable tracking or verify usage manually."
		if res.MonthlyCost > 0 {
			reason = fmt.Sprintf(
				"Storage account (~$%.0f/mo) has no last-access-time tracking — unable to determine if actively used. Enable tracking or verify usage.",
				res.MonthlyCost)
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "review",
			Reason:       reason,
			Risk:         engine.RiskLow,
			MonthlySave:  0,
			AutoExecute:  false,
		}

		if locked, lock := ctx.IsLocked(res.ID, res.ResourceGroup); locked {
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"LOCKED ("+lock.Level+" at "+lock.Scope+" scope)")
		}

		ApplySafety(ctx, &rec, res.ID, res.ResourceGroup)
		ApplyPolicy(&rec, policy)
		recs = append(recs, rec)
	}
	return recs
}
