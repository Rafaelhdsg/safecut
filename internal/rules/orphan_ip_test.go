package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/graph"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestOrphanIPRule_unassociated_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/ip1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:            id,
			Type:          "Microsoft.Network/publicIPAddresses",
			ResourceGroup: "rg",
			Properties:    map[string]interface{}{},
			MonthlyCost:   3.65,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
		Graph: graph.NewDependencyGraph(),
	}
	var r OrphanIPRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "deallocate" {
		t.Fatalf("expected deallocate rec, got %+v", recs)
	}
}

func TestOrphanIPRule_associated_skipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/ip2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Network/publicIPAddresses",
			Properties: map[string]interface{}{"ipConfiguration": map[string]interface{}{"id": "nic"}},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanIPRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("expected no recs for associated IP, got %d", len(recs))
	}
}

func TestOrphanIPRule_observeMode_skipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/ip3"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:         id,
			Type:       "Microsoft.Network/publicIPAddresses",
			Properties: map[string]interface{}{},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.ResourcePolicy{Mode: engine.ModeObserve}},
		},
	}
	var r OrphanIPRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("observe mode should suppress rec, got %d", len(recs))
	}
}
