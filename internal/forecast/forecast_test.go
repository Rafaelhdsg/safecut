package forecast

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/simulation"
)

// TestCalculate_TotalProjectedSavings locks the v1.0 rename of the
// Projection.ROI field (which was never real ROI) to
// Projection.TotalProjectedSavings. The number is a simple multiply,
// not net-of-cost ROI — the new field name reflects that.
func TestCalculate_TotalProjectedSavings(t *testing.T) {
	res := simulation.Result{
		Applied: []engine.Recommendation{
			{ResourceID: "r1", Action: "delete", MonthlySave: 100, AutoExecute: true, Risk: engine.RiskLow},
		},
		TotalSaving: 100,
	}
	p := Calculate(res, 12)

	if p.MonthlySaving != 100 {
		t.Errorf("MonthlySaving: got %v, want 100", p.MonthlySaving)
	}
	if p.TotalSaving != 1200 {
		t.Errorf("TotalSaving: got %v, want 1200", p.TotalSaving)
	}
	if p.TotalProjectedSavings != 1200 {
		t.Errorf("TotalProjectedSavings: got %v, want 1200", p.TotalProjectedSavings)
	}
}
