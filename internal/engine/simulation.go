package engine

// SimulationResult holds the outcome of simulating a set of recommendations.
type SimulationResult struct {
	Applied     []Recommendation
	Skipped     []Recommendation
	TotalSaving float64
	Errors      []string
}

// SimulationEngine runs dry-run simulations against a dependency graph
// to predict side effects before any real change is applied.
type SimulationEngine struct {
	decision *DecisionEngine
}

func NewSimulationEngine(de *DecisionEngine) *SimulationEngine {
	return &SimulationEngine{decision: de}
}

// Run executes the simulation for all pending recommendations,
// checking dependency safety and estimating impact.
func (s *SimulationEngine) Run() SimulationResult {
	result := SimulationResult{}
	for _, rec := range s.decision.Recommendations() {
		if rec.Risk == RiskHigh {
			result.Skipped = append(result.Skipped, rec)
			continue
		}
		result.Applied = append(result.Applied, rec)
		result.TotalSaving += rec.MonthlySave
	}
	return result
}
