package forecast

import "github.com/Rafaelhdsg/inframind-cli/internal/simulation"

// Projection holds cost-saving projections over a time period.
type Projection struct {
	Months        int
	MonthlySaving float64
	TotalSaving   float64
	ROI           float64
}

// Calculate produces a savings projection based on simulation results.
func Calculate(result simulation.Result, months int) Projection {
	monthly := result.TotalSaving
	total := monthly * float64(months)

	return Projection{
		Months:        months,
		MonthlySaving: monthly,
		TotalSaving:   total,
		ROI:           total, // simplified; refine with implementation cost later
	}
}
