package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestOrphanDiskRule_Evaluate_unattached(t *testing.T) {
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/orphan1"
	ctx := EvalContext{
		Resources: []providers.Resource{
			{
				ID:            diskID,
				Name:          "orphan1",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Properties:    map[string]interface{}{"diskState": "Unattached"},
				MonthlyCost:   12.0,
			},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			diskID: &engine.ResolvedPolicy{ResourcePolicy: engine.DefaultPolicy()},
		},
		Graph: graph.NewDependencyGraph(),
	}
	var rule OrphanDiskRule
	recs := rule.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("got %d recs, want 1", len(recs))
	}
	if recs[0].Action != "delete" {
		t.Errorf("Action = %q, want delete", recs[0].Action)
	}
	if recs[0].MonthlySave < 1 {
		t.Errorf("MonthlySave = %f, expected positive", recs[0].MonthlySave)
	}
}

// TestOrphanDiskRule_Evaluate_caseInsensitiveDiskState locks in the v1.0
// fix that made the diskState comparison case-insensitive. Azure Resource
// Graph sometimes returns "unattached" (lowercase) and the previous
// strict equality check silently dropped those from recommendations.
func TestOrphanDiskRule_Evaluate_caseInsensitiveDiskState(t *testing.T) {
	cases := []string{"Unattached", "unattached", "UNATTACHED", "UnAttached"}
	for _, state := range cases {
		diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d-" + state
		ctx := EvalContext{
			Resources: []providers.Resource{
				{
					ID:            diskID,
					Name:          "d",
					Type:          "Microsoft.Compute/disks",
					ResourceGroup: "rg",
					Properties:    map[string]interface{}{"diskState": state},
					MonthlyCost:   12.0,
				},
			},
			Policies: map[string]*engine.ResolvedPolicy{
				diskID: {ResourcePolicy: engine.DefaultPolicy()},
			},
			Graph: graph.NewDependencyGraph(),
		}
		markSafetyKnown(&ctx)
		var rule OrphanDiskRule
		recs := rule.Evaluate(ctx)
		if len(recs) != 1 {
			t.Fatalf("state=%q: got %d recs, want 1", state, len(recs))
		}
	}
}

// TestOrphanDiskRule_Evaluate_zeroCostFallback asserts that when the
// pricing API failed (MonthlyCost <= 0), the rule still fires but with
// MonthlySave=0 and a policy note, instead of the old hardcoded $1.54
// placeholder. This keeps TotalSaving honest.
func TestOrphanDiskRule_Evaluate_zeroCostFallback(t *testing.T) {
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/nopricing"
	ctx := EvalContext{
		Resources: []providers.Resource{
			{
				ID:            diskID,
				Name:          "nopricing",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Properties:    map[string]interface{}{"diskState": "Unattached"},
				MonthlyCost:   0, // pricing API returned no price
				PriceFallback: true,
			},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			diskID: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Graph: graph.NewDependencyGraph(),
	}
	markSafetyKnown(&ctx)
	var rule OrphanDiskRule
	recs := rule.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("expected 1 rec even without pricing, got %d", len(recs))
	}
	if recs[0].MonthlySave != 0 {
		t.Errorf("expected MonthlySave=0 fallback, got %v", recs[0].MonthlySave)
	}
	if recs[0].PolicyNote == "" {
		t.Errorf("expected non-empty PolicyNote explaining zero savings")
	}
}

func TestOrphanDiskRule_Evaluate_attachedSkipped(t *testing.T) {
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/attached"
	ctx := EvalContext{
		Resources: []providers.Resource{
			{
				ID:            diskID,
				Name:          "attached",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Properties:    map[string]interface{}{"diskState": "Attached"},
				MonthlyCost:   12.0,
			},
		},
		Policies: map[string]*engine.ResolvedPolicy{
			diskID: &engine.ResolvedPolicy{ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var rule OrphanDiskRule
	if recs := rule.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("expected no recs for attached disk, got %d", len(recs))
	}
}
