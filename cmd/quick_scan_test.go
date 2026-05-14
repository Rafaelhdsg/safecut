package cmd

import (
	"strings"
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/graph"
	"github.com/Rafaelhdsg/safecut/internal/pipeline"
	"github.com/Rafaelhdsg/safecut/pkg/report"
)

// Regression coverage for buildSafetyLine. The function is on a hot path
// (rendered once per recommendation in every `quick-scan` and `apply`) and
// misrendering a "lock present" or "snapshot available" label can mislead
// an operator into applying a destructive change, so every branch is
// pinned here.
func TestBuildSafetyLine(t *testing.T) {
	report.SetColor(false)
	defer report.SetColor(false)

	cases := []struct {
		name       string
		rec        engine.Recommendation
		out        *pipeline.Output
		wantPieces []string
		notWant    []string
	}{
		{
			name: "auto-safe low risk",
			rec: engine.Recommendation{
				ResourceID:  "/r/a",
				Risk:        engine.RiskLow,
				AutoExecute: true,
			},
			wantPieces: []string{"auto-apply safe", "risk=low"},
			notWant:    []string{"manual review", "lock present", "snapshot available"},
		},
		{
			name: "manual review on high risk",
			rec: engine.Recommendation{
				ResourceID:  "/r/b",
				Risk:        engine.RiskHigh,
				AutoExecute: false,
			},
			wantPieces: []string{"manual review required", "risk=high"},
		},
		{
			name: "medium risk label",
			rec: engine.Recommendation{
				Risk:        engine.RiskMedium,
				AutoExecute: true,
			},
			wantPieces: []string{"risk=medium"},
		},
		{
			name: "policy note locked -> lock present",
			rec: engine.Recommendation{
				Risk:        engine.RiskLow,
				AutoExecute: false,
				PolicyNote:  "Resource is LOCKED by management lock",
			},
			wantPieces: []string{"manual review required", "lock present"},
		},
		{
			name: "policy note snapshot -> snapshot available",
			rec: engine.Recommendation{
				Risk:        engine.RiskLow,
				AutoExecute: true,
				PolicyNote:  "Disk snapshot exists; safe to delete",
			},
			wantPieces: []string{"snapshot available"},
		},
		{
			name: "dependents from graph",
			rec: engine.Recommendation{
				ResourceID:  "/r/parent",
				Risk:        engine.RiskLow,
				AutoExecute: true,
			},
			out:        fakeOutputWithDependents("/r/parent", 2),
			wantPieces: []string{"2 dependents"},
		},
		{
			name: "nil graph does not panic",
			rec: engine.Recommendation{
				Risk:        engine.RiskLow,
				AutoExecute: true,
			},
			out:        &pipeline.Output{},
			wantPieces: []string{"auto-apply safe"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSafetyLine(tc.rec, tc.out)
			for _, want := range tc.wantPieces {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in safety line: %q", want, got)
				}
			}
			for _, nope := range tc.notWant {
				if strings.Contains(got, nope) {
					t.Errorf("unexpected %q in safety line: %q", nope, got)
				}
			}
		})
	}
}

func fakeOutputWithDependents(parentID string, n int) *pipeline.Output {
	g := graph.NewDependencyGraph()
	parent := &graph.Node{ID: parentID, Type: "Microsoft.Compute/disks"}
	g.AddNode(parent)
	for i := 0; i < n; i++ {
		child := &graph.Node{ID: parentID + "/child/" + string(rune('a'+i))}
		g.AddNode(child)
		g.Link(parentID, child.ID)
	}
	return &pipeline.Output{Graph: g}
}
