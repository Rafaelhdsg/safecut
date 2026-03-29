package simulation

import (
	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
)

// Result holds the outcome of simulating a set of recommendations.
type Result struct {
	Applied     []engine.Recommendation
	Skipped     []engine.Recommendation
	TotalSaving float64
	Errors      []string
}

// Engine runs dry-run simulations against a dependency graph
// to predict side effects before any real change is applied.
type Engine struct {
	decision *engine.DecisionEngine
	graph    *graph.DependencyGraph
}

func NewEngine(de *engine.DecisionEngine, dg *graph.DependencyGraph) *Engine {
	return &Engine{decision: de, graph: dg}
}

// Run executes the simulation for all pending recommendations,
// checking dependency safety and estimating impact.
func (s *Engine) Run() Result {
	result := Result{}
	for _, rec := range s.decision.Recommendations() {
		if rec.Risk == engine.RiskHigh {
			result.Skipped = append(result.Skipped, rec)
			continue
		}

		if !s.isSafeToApply(rec) {
			rec.Risk = engine.RiskHigh
			result.Skipped = append(result.Skipped, rec)
			result.Errors = append(result.Errors, "resource "+rec.ResourceID+" has active dependents")
			continue
		}

		result.Applied = append(result.Applied, rec)
		result.TotalSaving += rec.MonthlySave
	}
	return result
}

// isSafeToApply checks the dependency graph to ensure removing/changing
// a resource won't break anything that depends on it.
func (s *Engine) isSafeToApply(rec engine.Recommendation) bool {
	if s.graph == nil {
		return true
	}
	node, ok := s.graph.GetNode(rec.ResourceID)
	if !ok {
		return true
	}
	return len(node.Children) == 0
}
