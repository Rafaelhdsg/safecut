package rules

import (
	"github.com/Rafaelhdsg/inframind-cli/internal/discovery"
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// EvalContext provides all data a rule needs to make a decision.
type EvalContext struct {
	Resources []providers.Resource
	Metrics   map[string]*discovery.ResourceMetrics
	Analyses  map[string]*engine.IdleAnalysis
	Policies  map[string]*engine.ResolvedPolicy
}

// Rule is the common interface for all optimization rules.
type Rule interface {
	Name() string
	Evaluate(ctx EvalContext) []engine.Recommendation
}

// ApplyPolicy applies governance policy to a recommendation:
// bumps risk, sets auto-execute flag, and adds policy notes.
func ApplyPolicy(rec *engine.Recommendation, policy *engine.ResolvedPolicy) {
	if policy == nil {
		rec.AutoExecute = true
		return
	}

	rec.Risk = engine.BumpRisk(rec.Risk, policy.RiskAdjustment())
	rec.AutoExecute = !policy.BlocksAutoExecution()

	if policy.ExternalDeps {
		rec.PolicyNote = appendNote(rec.PolicyNote, "external dependencies detected — reduced confidence, manual review required")
	}
	if policy.Criticality == engine.CriticalityHigh {
		rec.PolicyNote = appendNote(rec.PolicyNote, "high criticality — auto-execution blocked")
	}
	if policy.Mode == engine.ModeProtect {
		rec.PolicyNote = appendNote(rec.PolicyNote, "protect mode — recommendation only, no auto-action")
	}
}

func appendNote(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}
