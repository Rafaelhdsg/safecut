package forecast

import "github.com/Rafaelhdsg/inframind-cli/internal/simulation"

// Projection holds cost-saving projections over a time period.
//
// TotalProjectedSavings is the monthly saving extrapolated over `Months`.
// It does NOT model compounding, inflation, implementation cost, or the
// cost of InfraMind itself — it's a straight multiplication. The field
// used to be called `ROI`, but that name was misleading: true ROI would
// be (gain - cost) / cost. We renamed it for v1.0 so the CLI output
// matches what the number actually represents.
type Projection struct {
	Months                int
	MonthlySaving         float64
	TotalSaving           float64
	TotalProjectedSavings float64
}

// Calculate produces a savings projection based on simulation results.
func Calculate(result simulation.Result, months int) Projection {
	monthly := result.TotalSaving
	total := monthly * float64(months)

	return Projection{
		Months:                months,
		MonthlySaving:         monthly,
		TotalSaving:           total,
		TotalProjectedSavings: total,
	}
}
