package simulation

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
)

func TestEngine_Run_applyRecommendation(t *testing.T) {
	de := engine.NewDecisionEngine()
	de.AddRecommendation(engine.Recommendation{
		ResourceID:  "/sub/rg/Microsoft.Compute/disks/orphan",
		Action:      "delete",
		Risk:        engine.RiskLow,
		MonthlySave: 12.50,
		AutoExecute: true,
	})
	result := NewEngine(de, graph.NewDependencyGraph()).Run()
	if len(result.Applied) != 1 || result.TotalSaving != 12.50 {
		t.Fatalf("expected 1 applied and 12.50 saving, got %+v", result)
	}
}

func TestEngine_Run_skipHighRisk(t *testing.T) {
	de := engine.NewDecisionEngine()
	de.AddRecommendation(engine.Recommendation{
		ResourceID: "/sub/rg/Microsoft.Compute/disks/locked",
		Action:     "delete",
		Risk:       engine.RiskHigh,
	})
	result := NewEngine(de, graph.NewDependencyGraph()).Run()
	if len(result.Skipped) != 1 || result.RisksFound != 1 {
		t.Fatalf("expected high-risk rec to be skipped, got %+v", result)
	}
}

// TestEngine_Run_dedupeSameResource covers the v1.0 fix that prevents
// double-counting when multiple rules fire on the same resource.
// Before the dedupe pass, a disk flagged both by OrphanDiskRule and
// IdleResourceRule would contribute its savings twice, inflating
// TotalSaving. We assert:
//  1. Only the highest-priority action (delete > stop > rightsize) wins.
//  2. TotalSaving counts the winner's savings exactly once.
//  3. The losing rec lands in SuppressedByDedup with a Reason.
func TestEngine_Run_dedupeSameResource(t *testing.T) {
	de := engine.NewDecisionEngine()
	resID := "/sub/rg/Microsoft.Compute/virtualMachines/vm1"

	// Two recs for the same resource. "delete" has higher priority.
	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "stop",
		Risk:        engine.RiskLow,
		MonthlySave: 50,
		AutoExecute: true,
	})
	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "delete",
		Risk:        engine.RiskLow,
		MonthlySave: 100,
		AutoExecute: true,
	})

	result := NewEngine(de, graph.NewDependencyGraph()).Run()

	if len(result.Applied) != 1 {
		t.Fatalf("expected 1 applied after dedupe, got %d", len(result.Applied))
	}
	if result.Applied[0].Action != "delete" {
		t.Fatalf("expected delete to win, got %q", result.Applied[0].Action)
	}
	if result.TotalSaving != 100 {
		t.Fatalf("expected TotalSaving=100 (winner only), got %v", result.TotalSaving)
	}
	if len(result.SuppressedByDedup) != 1 {
		t.Fatalf("expected 1 suppressed, got %d", len(result.SuppressedByDedup))
	}
	if result.SuppressedByDedup[0].Rec.Action != "stop" {
		t.Fatalf("expected stop to be suppressed, got %q", result.SuppressedByDedup[0].Rec.Action)
	}
	if result.SuppressedByDedup[0].Reason == "" {
		t.Fatal("expected non-empty suppression reason")
	}
}

// TestEngine_Run_dedupePicksHighestSave covers the tiebreaker: when
// two recs have the same action priority, the higher MonthlySave wins.
func TestEngine_Run_dedupePicksHighestSave(t *testing.T) {
	de := engine.NewDecisionEngine()
	resID := "/sub/rg/Microsoft.Compute/virtualMachines/vm2"

	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "rightsize",
		Risk:        engine.RiskLow,
		MonthlySave: 20,
		AutoExecute: true,
	})
	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "rightsize",
		Risk:        engine.RiskLow,
		MonthlySave: 45,
		AutoExecute: true,
	})

	result := NewEngine(de, graph.NewDependencyGraph()).Run()

	if result.TotalSaving != 45 {
		t.Fatalf("expected tiebreaker to pick $45, got %v", result.TotalSaving)
	}
}

// TestEngine_Run_dedupeRightsizeBeatsReserve documents that rightsize
// has a higher action priority than reserve, so rightsize wins even
// when reserve has larger MonthlySave (the two are mutually exclusive
// on the same resource and rightsize is reversible).
func TestEngine_Run_dedupeRightsizeBeatsReserve(t *testing.T) {
	de := engine.NewDecisionEngine()
	resID := "/sub/rg/Microsoft.Compute/virtualMachines/vm3"

	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "reserve",
		Risk:        engine.RiskLow,
		MonthlySave: 45,
		AutoExecute: true,
	})
	de.AddRecommendation(engine.Recommendation{
		ResourceID:  resID,
		Action:      "rightsize",
		Risk:        engine.RiskLow,
		MonthlySave: 20,
		AutoExecute: true,
	})

	result := NewEngine(de, graph.NewDependencyGraph()).Run()
	if result.TotalSaving != 20 {
		t.Fatalf("expected rightsize (20) to win over reserve by priority, got %v", result.TotalSaving)
	}
	if len(result.SuppressedByDedup) != 1 || result.SuppressedByDedup[0].Rec.Action != "reserve" {
		t.Fatalf("expected reserve suppressed, got %+v", result.SuppressedByDedup)
	}
}

func TestEngine_Run_dependentsBlock(t *testing.T) {
	dg := graph.NewDependencyGraph()
	dg.AddNode(&graph.Node{ID: "parent", Name: "parent-disk", Type: "Microsoft.Compute/disks"})
	dg.AddNode(&graph.Node{ID: "child", Name: "snapshot", Type: "Microsoft.Compute/snapshots"})
	dg.Link("parent", "child")

	de := engine.NewDecisionEngine()
	de.AddRecommendation(engine.Recommendation{
		ResourceID: "parent",
		Action:     "delete",
		Risk:       engine.RiskLow,
	})
	result := NewEngine(de, dg).Run()
	if len(result.Skipped) != 1 {
		t.Fatalf("expected parent with dependents to be skipped, got %+v", result)
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected dependents error message")
	}
}
