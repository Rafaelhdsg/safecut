package simulation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
)

// SuppressedRec records a recommendation that was dropped from the
// Applied set (and from TotalSaving) because another, higher-priority
// recommendation targets the same resource. Surfacing these lets the
// operator see exactly which savings were absorbed, so the total never
// mysteriously diverges from the sum of visible line items.
type SuppressedRec struct {
	Rec    engine.Recommendation
	Reason string
}

// Result holds the outcome of simulating a set of recommendations.
type Result struct {
	Applied           []engine.Recommendation
	Skipped           []engine.Recommendation
	SuppressedByDedup []SuppressedRec
	TotalSaving       float64
	RisksFound        int
	Errors            []string
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
//
// Before totaling savings, Run deduplicates recommendations that
// target the same resource. This is the only place TotalSaving is
// computed, so dedup here guarantees the number the user sees never
// double-counts a resource that matched two rules (e.g. a disk
// matched by OrphanDisk and IdleResource, or a VM matched by
// Rightsize and ReservedInstance).
func (s *Engine) Run() Result {
	result := Result{}

	applied, suppressed := dedupe(s.decision.Recommendations())
	result.SuppressedByDedup = suppressed

	for _, rec := range applied {
		if rec.Risk == engine.RiskHigh {
			result.Skipped = append(result.Skipped, rec)
			result.RisksFound++
			continue
		}

		safe, reason := s.checkSafety(rec)
		if !safe {
			rec.Risk = engine.RiskHigh
			rec.AutoExecute = false
			rec.PolicyNote = appendNote(rec.PolicyNote, reason)
			result.Skipped = append(result.Skipped, rec)
			result.RisksFound++
			result.Errors = append(result.Errors, reason)
			continue
		}

		result.Applied = append(result.Applied, rec)
		result.TotalSaving += rec.MonthlySave
	}
	return result
}

// actionPriority returns a dedup priority for an action. Higher wins.
// The ordering reflects the economic reality:
//   - delete absorbs every other saving on the same resource (no VM
//     left to rightsize or reserve)
//   - stop absorbs rightsize / reserve (VM turned off)
//   - reserve and rightsize are mutually exclusive (can't reserve a
//     size you're about to abandon); the larger saving wins, and a tie
//     goes to rightsize because it's reversible
//   - review/downgrade/deallocate are informational in nature
func actionPriority(action string) int {
	switch strings.ToLower(action) {
	case "delete":
		return 100
	case "stop":
		return 80
	case "deallocate":
		return 70
	case "rightsize":
		return 50
	case "reserve":
		return 40
	case "downgrade":
		return 30
	case "review":
		return 10
	default:
		return 1
	}
}

// dedupe collapses recommendations that target the same resource into
// a single winner and returns the suppressed ones separately. The
// winner is chosen by (1) action priority, (2) MonthlySave magnitude.
// Input order is preserved for ties so the output is deterministic.
func dedupe(recs []engine.Recommendation) ([]engine.Recommendation, []SuppressedRec) {
	type indexedRec struct {
		rec engine.Recommendation
		pos int
	}
	byResource := make(map[string][]indexedRec)
	order := make([]string, 0)
	for i, r := range recs {
		if _, ok := byResource[r.ResourceID]; !ok {
			order = append(order, r.ResourceID)
		}
		byResource[r.ResourceID] = append(byResource[r.ResourceID], indexedRec{rec: r, pos: i})
	}

	var applied []engine.Recommendation
	var suppressed []SuppressedRec

	for _, resID := range order {
		group := byResource[resID]
		if len(group) == 1 {
			applied = append(applied, group[0].rec)
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			pi := actionPriority(group[i].rec.Action)
			pj := actionPriority(group[j].rec.Action)
			if pi != pj {
				return pi > pj
			}
			if group[i].rec.MonthlySave != group[j].rec.MonthlySave {
				return group[i].rec.MonthlySave > group[j].rec.MonthlySave
			}
			return group[i].pos < group[j].pos
		})
		winner := group[0].rec
		applied = append(applied, winner)
		for _, loser := range group[1:] {
			suppressed = append(suppressed, SuppressedRec{
				Rec: loser.rec,
				Reason: fmt.Sprintf(
					"suppressed: action %q overridden by %q on the same resource (savings absorbed to avoid double counting)",
					loser.rec.Action, winner.Action,
				),
			})
		}
	}

	return applied, suppressed
}

// checkSafety performs multi-layer safety checks against the dependency graph.
func (s *Engine) checkSafety(rec engine.Recommendation) (bool, string) {
	if s.graph == nil {
		return true, ""
	}
	node, ok := s.graph.GetNode(rec.ResourceID)
	if !ok {
		return true, ""
	}

	// Block: resource has children depending on it
	if len(node.Children) > 0 {
		names := make([]string, 0, len(node.Children))
		for _, c := range node.Children {
			names = append(names, c.Name)
		}
		return false, fmt.Sprintf(
			"resource %s has %d active dependents (%s) — action blocked for safety",
			node.Name, len(node.Children), strings.Join(names, ", "),
		)
	}

	return true, ""
}

func appendNote(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}
