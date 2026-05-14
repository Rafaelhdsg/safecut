package rules

import (
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/discovery"
	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/graph"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// isType does a case-insensitive resource type match.
// Resource Graph returns lowercase; ARM returns mixed case.
func isType(resource providers.Resource, resourceType string) bool {
	return strings.EqualFold(resource.Type, resourceType)
}

// EvalContext provides all data a rule needs to make a decision.
type EvalContext struct {
	Resources []providers.Resource
	Metrics   map[string]*discovery.ResourceMetrics
	Analyses  map[string]*engine.IdleAnalysis
	Policies  map[string]*engine.ResolvedPolicy
	Graph     *graph.DependencyGraph
	Locks     map[string][]providers.LockInfo
	Snapshots map[string][]providers.SnapshotInfo
	Pricing   pricing.PricingProvider

	// LocksStatus / SnapshotsStatus mirror discovery.Snapshot and let
	// rules reason about probe failures. SafetyProviderSupported is
	// true when the underlying provider advertises SafetyProvider —
	// it lets rules distinguish "CLI doesn't support locks on this
	// cloud" from "we tried and failed".
	LocksStatus             map[string]discovery.SafetyStatus
	SnapshotsStatus         map[string]discovery.SafetyStatus
	SafetyProviderSupported bool

	// PricingWarnings collects human-readable messages about pricing
	// lookups that failed during rule evaluation. Rules call
	// RecordPricingWarning instead of writing directly so the pipeline
	// can decide how to surface them. The field is a pointer so every
	// value-copy of EvalContext that rules receive writes into the
	// same underlying slice.
	PricingWarnings *[]string
}

// RecordPricingWarning appends a warning about a failed price lookup.
// Safe to call when PricingWarnings is nil (no-op); that lets unit
// tests ignore the plumbing.
func (ctx EvalContext) RecordPricingWarning(msg string) {
	if ctx.PricingWarnings == nil {
		return
	}
	*ctx.PricingWarnings = append(*ctx.PricingWarnings, msg)
}

// IsLocked returns true if the resource or its RG has a CanNotDelete or ReadOnly lock.
func (ctx EvalContext) IsLocked(resourceID, resourceGroup string) (bool, providers.LockInfo) {
	if locks, ok := ctx.Locks[resourceGroup]; ok {
		for _, l := range locks {
			if l.Level == "CanNotDelete" || l.Level == "ReadOnly" {
				return true, l
			}
		}
	}
	if locks, ok := ctx.Locks[resourceID]; ok {
		for _, l := range locks {
			if l.Level == "CanNotDelete" || l.Level == "ReadOnly" {
				return true, l
			}
		}
	}
	return false, providers.LockInfo{}
}

// LockStateUnknown returns true when we cannot vouch for the lock
// status of a resource — either the per-resource probe errored, the
// enclosing RG probe errored, or the provider supports safety checks
// but no probe ran for this scope. Rules must NOT auto-execute when
// this is true: the whole point of the safety layer is to fail closed.
func (ctx EvalContext) LockStateUnknown(resourceID, resourceGroup string) bool {
	if !ctx.SafetyProviderSupported {
		// Provider can't tell us — downgrade to manual review.
		return true
	}
	if ctx.LocksStatus == nil {
		return true
	}
	if st, ok := ctx.LocksStatus[resourceID]; ok && st == discovery.SafetyDenied {
		return true
	}
	if resourceGroup != "" {
		if st, ok := ctx.LocksStatus[resourceGroup]; ok && st == discovery.SafetyDenied {
			return true
		}
		// We consider an RG whose probe never ran as unknown too:
		// there is no valid reason for the collector to skip a RG
		// when safety is supported except a bug we want to detect.
		if _, ok := ctx.LocksStatus[resourceGroup]; !ok {
			return true
		}
	}
	if _, ok := ctx.LocksStatus[resourceID]; !ok {
		return true
	}
	return false
}

// HasSnapshots returns true if the resource group has disk snapshots.
func (ctx EvalContext) HasSnapshots(resourceGroup string) bool {
	snaps, ok := ctx.Snapshots[resourceGroup]
	return ok && len(snaps) > 0
}

// SnapshotStateUnknown returns true when the snapshot probe for the RG
// failed or never ran. Callers that use snapshot presence as a safety
// guard (e.g. orphan-disk) must treat this as "assume snapshots exist"
// — again, fail closed.
func (ctx EvalContext) SnapshotStateUnknown(resourceGroup string) bool {
	if !ctx.SafetyProviderSupported {
		return true
	}
	if ctx.SnapshotsStatus == nil {
		return true
	}
	st, ok := ctx.SnapshotsStatus[resourceGroup]
	if !ok {
		// No probe recorded for this RG — either no disks in it, or
		// a bug. Either way, err on the safe side.
		return false
	}
	return st == discovery.SafetyDenied
}

// Rule is the common interface for all optimization rules.
type Rule interface {
	Name() string
	Evaluate(ctx EvalContext) []engine.Recommendation
}

// ApplyPolicy applies governance policy to a recommendation:
// bumps risk, sets auto-execute flag, and adds policy notes.
//
// Auto-execute is combined with the rule's own prior verdict via
// logical AND — a rule (or ApplySafety) that already marked
// AutoExecute=false is never re-enabled here. Only a policy that
// actively blocks auto-execution can downgrade the flag further.
func ApplyPolicy(rec *engine.Recommendation, policy *engine.ResolvedPolicy) {
	if policy == nil {
		return
	}

	rec.Risk = engine.BumpRisk(rec.Risk, policy.RiskAdjustment())
	if policy.BlocksAutoExecution() {
		rec.AutoExecute = false
	}

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

// ApplySafety downgrades a recommendation when the lock probe failed
// or was never attempted. It MUST be called by every rule right
// before emitting, so a single-line mistake in a new rule can never
// produce "auto-apply safe" on a locked production resource.
//
// The function is conservative by design:
//   - unknown lock state → AutoExecute=false + risk bumped one notch
//   - known lock present is already handled by rule-specific IsLocked
//     calls; this function does not re-apply it.
func ApplySafety(ctx EvalContext, rec *engine.Recommendation, resourceID, resourceGroup string) {
	if !ctx.LockStateUnknown(resourceID, resourceGroup) {
		return
	}
	rec.AutoExecute = false
	rec.Risk = engine.BumpRisk(rec.Risk, 1)
	rec.PolicyNote = appendNote(rec.PolicyNote, "lock state unknown — manual review required (missing Microsoft.Authorization/locks/read)")
}

func appendNote(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}

// extractProvisioningState returns the ARM provisioningState string
// from a resource's properties map, or "" if absent. Kept here
// because several rules use it to detect transient states (Updating,
// Creating, Deleting, Failed) and need a single, consistent accessor
// that handles both top-level and nested property shapes.
func extractProvisioningState(props map[string]interface{}) string {
	if props == nil {
		return ""
	}
	if s, ok := props["provisioningState"].(string); ok {
		return s
	}
	if inner, ok := props["properties"].(map[string]interface{}); ok {
		if s, ok := inner["provisioningState"].(string); ok {
			return s
		}
	}
	return ""
}
