package rules

import (
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/discovery"
	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

func TestReservedInstanceRule_steadyRunningVM_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-ri"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:          id,
			Type:        "Microsoft.Compute/virtualMachines",
			PowerState:  "running",
			Properties:  map[string]interface{}{"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"}},
			MonthlyCost: 140.0,
		}},
		Metrics: map[string]*discovery.ResourceMetrics{
			id: {
				CPUAvgPercent: 25,
				IdleDays:      0,
				ObservedDays:  14,
				Status:        discovery.MetricsKnown,
			},
		},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.2, Confidence: 0.9, Signals: []engine.SignalResult{{Name: "cpu", Value: 25}}},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Pricing: &fakePricing{
			Reservations: map[string]float64{
				"Standard_D4s_v3|1 Year":  90, // $90/mo amortized
				"Standard_D4s_v3|3 Years": 65,
			},
		},
	}
	markSafetyKnown(&ctx)
	r := DefaultReservedInstanceRule()
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "reserve" {
		t.Fatalf("expected reserve rec, got %+v", recs)
	}
	if recs[0].MonthlySave <= 0 {
		t.Fatalf("expected positive savings, got %v", recs[0].MonthlySave)
	}
}

// TestReservedInstanceRule_failsClosedWithoutPricing locks the v1.0
// behaviour: without a working reservation-price lookup, the rule must
// skip the rec and emit a PricingWarning. The old code multiplied the
// monthly cost by a hardcoded 0.36 discount, which produced plausible
// but fictional numbers.
func TestReservedInstanceRule_failsClosedWithoutPricing(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-ri-nopricing"
	warnings := []string{}
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:          id,
			Type:        "Microsoft.Compute/virtualMachines",
			PowerState:  "running",
			Properties:  map[string]interface{}{"hardwareProfile": map[string]interface{}{"vmSize": "Standard_D4s_v3"}},
			MonthlyCost: 140.0,
		}},
		Metrics: map[string]*discovery.ResourceMetrics{
			id: {CPUAvgPercent: 25, ObservedDays: 14, Status: discovery.MetricsKnown},
		},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.2, Confidence: 0.9, Signals: []engine.SignalResult{{Name: "cpu", Value: 25}}},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Pricing:         &fakePricing{}, // no reservation entries seeded
		PricingWarnings: &warnings,
	}
	markSafetyKnown(&ctx)
	r := DefaultReservedInstanceRule()
	recs := r.Evaluate(ctx)
	if len(recs) != 0 {
		t.Fatalf("expected no rec when reservation pricing missing, got %d", len(recs))
	}
	if len(warnings) == 0 {
		t.Fatal("expected a PricingWarning about the reservation lookup")
	}
}

// TestReservationTerm_Constants locks the enum values so the filter
// string we send to the Retail API never silently drifts — a typo from
// "1 Year" to "1 year" would match zero rows and every RI rec would
// disappear.
func TestReservationTerm_Constants(t *testing.T) {
	if pricing.Reservation1Year != "1 Year" {
		t.Errorf("Reservation1Year: got %q, want \"1 Year\"", pricing.Reservation1Year)
	}
	if pricing.Reservation3Years != "3 Years" {
		t.Errorf("Reservation3Years: got %q, want \"3 Years\"", pricing.Reservation3Years)
	}
}

func TestReservedInstanceRule_idleVM_skipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-idle"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:          id,
			Type:        "Microsoft.Compute/virtualMachines",
			PowerState:  "running",
			MonthlyCost: 140.0,
		}},
		Metrics: map[string]*discovery.ResourceMetrics{
			id: {CPUAvgPercent: 1},
		},
		Analyses: map[string]*engine.IdleAnalysis{
			id: {Score: 0.9, Confidence: 0.9, Signals: []engine.SignalResult{{Name: "cpu", Value: 1}}},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultReservedInstanceRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("idle VM should be skipped for RI, got %d", len(recs))
	}
}

func TestReservedInstanceRule_deallocatedSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-off"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Compute/virtualMachines",
			PowerState: "deallocated",
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	r := DefaultReservedInstanceRule()
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("deallocated VM should be skipped, got %d", len(recs))
	}
}
