package forecast

import "github.com/Rafaelhdsg/inframind-cli/internal/engine"

// Projection holds cost-saving projections over a time period.
type Projection struct {
	Months        int
	MonthlySaving float64
	TotalSaving   float64
	ROI           float64
}

// Calculate produces a savings projection based on simulation results.
func Calculate(result engine.SimulationResult, months int) Projection {
	monthly := result.TotalSaving
	total := monthly * float64(months)

	return Projection{
		Months:        months,
		MonthlySaving: monthly,
		TotalSaving:   total,
		ROI:           total, // simplified; refine with implementation cost later
	}
}
