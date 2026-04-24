package rules

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// IdleContainerGroupRule detects container groups in Stopped or Failed
// state that are still allocated and incurring base costs.
type IdleContainerGroupRule struct{}

func (r *IdleContainerGroupRule) Name() string {
	return "idle-container-group"
}

func (r *IdleContainerGroupRule) Evaluate(ctx EvalContext) []engine.Recommendation {
	var recs []engine.Recommendation
	for _, res := range ctx.Resources {
		if !isType(res, "Microsoft.ContainerInstance/containerGroups") {
			continue
		}

		policy := ctx.Policies[res.ID]
		if policy != nil && policy.Mode == engine.ModeObserve {
			continue
		}

		instanceState := extractInstanceState(res.Properties)
		provisioningState, _ := res.Properties["provisioningState"].(string)

		isIdle := strings.EqualFold(instanceState, "Stopped") ||
			strings.EqualFold(instanceState, "Failed") ||
			strings.EqualFold(provisioningState, "Failed")

		if !isIdle {
			continue
		}

		reason := fmt.Sprintf(
			"Container Group in %s state — still incurring base allocation costs",
			instanceState)
		if strings.EqualFold(provisioningState, "Failed") && instanceState == "" {
			reason = "Container Group provisioning failed — not running but still allocated"
		}

		rec := engine.Recommendation{
			ResourceID:   res.ID,
			ResourceType: res.Type,
			Action:       "delete",
			Reason:       reason,
			Risk:         engine.RiskLow,
			MonthlySave:  res.MonthlyCost,
			AutoExecute:  true,
		}

		// Failed containers are almost always there because a human
		// is debugging. Auto-deleting them would wipe the failure
		// evidence (logs, exit codes) and burn trust instantly even
		// though the cost figures would be "right". The rec still
		// surfaces so the operator sees the cost, but action is
		// never automated.
		if strings.EqualFold(instanceState, "Failed") || strings.EqualFold(provisioningState, "Failed") {
			rec.AutoExecute = false
			rec.Risk = engine.BumpRisk(rec.Risk, 1)
			rec.PolicyNote = appendNote(rec.PolicyNote,
				"failed state may be intentional for debug; manual review before delete")
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

func extractInstanceState(props map[string]interface{}) string {
	iv, ok := props["instanceView"].(map[string]interface{})
	if !ok {
		return ""
	}
	state, _ := iv["state"].(string)
	return state
}
