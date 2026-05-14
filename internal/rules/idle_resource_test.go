package rules

import (
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

func TestIdleResourceRule_vmIdle_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-idle"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.95, Confidence: 0.95},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultIdleResourceRule()
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "stop" {
		t.Fatalf("expected stop rec for idle VM, got %+v", recs)
	}
}

func TestIdleResourceRule_deallocatedVMSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-off"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "deallocated",
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.99, Confidence: 0.95},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultIdleResourceRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("deallocated VM has no compute cost to save, got %d recs", len(recs))
	}
}

// TestIdleResourceRule_skipsDisksAndIPs is the regression test for the
// v1.0 dedup fix: OrphanDiskRule and OrphanIPRule already emit
// dedicated delete recommendations for unattached disks and public IPs.
// If IdleResourceRule also fires on them, the simulation sums the same
// saving twice and TotalSaving is inflated. This test asserts the
// generic rule stays out of the way.
func TestIdleResourceRule_skipsDisksAndIPs(t *testing.T) {
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d1"
	ipID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/ip1"
	ctx := EvalContext{
		Resources: []providers.Resource{
			{ID: diskID, Type: "Microsoft.Compute/disks", Properties: map[string]interface{}{"diskState": "Unattached"}},
			{ID: ipID, Type: "Microsoft.Network/publicIPAddresses", Properties: map[string]interface{}{"ipConfiguration": nil}},
		},
		Analyses: map[string]*engine.IdleAnalysis{
			diskID: {Score: 0.99, Confidence: 0.95},
			ipID:   {Score: 0.99, Confidence: 0.95},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			diskID: {ResourcePolicy: engine.DefaultPolicy()},
			ipID:   {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultIdleResourceRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("IdleResourceRule must skip disks/IPs (handled by dedicated rules), got %d recs", len(recs))
	}
}

// TestIdleResourceRule_emptyPowerStateNotAuto exercises the b4-powerstate
// safety fix: when a VM's PowerState is empty (Azure API sometimes
// returns nothing for VMs in transitional states), we still emit a rec
// but NEVER mark it AutoExecute=true — auto-stopping a VM whose real
// state we can't confirm is exactly the kind of silent mistake v1.0
// is supposed to prevent.
func TestIdleResourceRule_emptyPowerStateNotAuto(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-unknown"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "", // unknown — must not be auto-stopped
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.95, Confidence: 0.95},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	markSafetyKnown(&ctx)
	r := DefaultIdleResourceRule()
	recs := r.Evaluate(ctx)
	if len(recs) == 0 {
		t.Fatal("expected a rec even with unknown power state, got none")
	}
	if recs[0].AutoExecute {
		t.Error("empty PowerState rec must not be AutoExecute=true")
	}
	if recs[0].PolicyNote == "" {
		t.Error("expected a PolicyNote explaining the manual-review requirement")
	}
}

func TestIdleResourceRule_lowConfidenceSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-new"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.9, Confidence: 0.3},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultIdleResourceRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("low-confidence analysis should not produce rec, got %d", len(recs))
	}
}
