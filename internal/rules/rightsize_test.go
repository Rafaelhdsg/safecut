package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestRightsizeRule_oversizedVM_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-big"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
			Properties: map[string]interface{}{
				"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"},
			},
			Location: "eastus",
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {
				Score:      0.5,
				Confidence: 0.9,
				Signals:    []engine.SignalResult{{Name: "cpu", Value: 10}},
			},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Pricing: &fakePricing{
			VMs: map[string]float64{
				"Standard_D4s_v3": 200, // current
				"Standard_D2s_v3": 100, // target
			},
		},
	}
	r := DefaultRightsizeRule()
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "rightsize" {
		t.Fatalf("expected rightsize rec, got %+v", recs)
	}
	if recs[0].MonthlySave <= 0 {
		t.Fatalf("expected positive savings, got %f", recs[0].MonthlySave)
	}
}

// TestRightsizeRule_failsClosedWhenPricingMissing locks in the v1.0
// behaviour: if the pricing API has no row for either the current or
// the target VM size, the rule must record a PricingWarning and skip
// the rec entirely. Previously, the rule used a hardcoded vmSizeDB
// that would happily produce a recommendation with a fabricated
// "typical" price.
func TestRightsizeRule_failsClosedWhenPricingMissing(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-no-price"
	warnings := []string{}
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
			Properties: map[string]interface{}{
				"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"},
			},
			Location: "eastus",
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.5, Confidence: 0.9, Signals: []engine.SignalResult{{Name: "cpu", Value: 10}}},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Pricing:         &fakePricing{}, // no entries → GetVMPrice returns 0, nil
		PricingWarnings: &warnings,
	}
	r := DefaultRightsizeRule()
	recs := r.Evaluate(ctx)
	if len(recs) != 0 {
		t.Fatalf("expected 0 recs when pricing missing, got %d", len(recs))
	}
	if len(warnings) == 0 {
		t.Fatal("expected a PricingWarning to be recorded")
	}
}

func TestRightsizeRule_highCPU_skipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-busy"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
			Properties: map[string]interface{}{
				"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"},
			},
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {
				Score:      0.3,
				Confidence: 0.9,
				Signals:    []engine.SignalResult{{Name: "cpu", Value: 70}},
			},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultRightsizeRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("busy VM should not be rightsized, got %d", len(recs))
	}
}

func TestRightsizeRule_fullyIdleSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-zero"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "running",
			Properties: map[string]interface{}{
				"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"},
			},
		}},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.95, Confidence: 0.9, Signals: []engine.SignalResult{{Name: "cpu", Value: 0.1}}},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultRightsizeRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("fully idle VM should be handled by IdleResourceRule, got %d rightsize recs", len(recs))
	}
}
