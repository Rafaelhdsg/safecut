package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestOrphanLoadBalancerRule_standardNoBackends(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/lb1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Network/loadBalancers",
			Properties: map[string]interface{}{
				"sku":                 map[string]interface{}{"name": "Standard"},
				"backendAddressPools": []interface{}{},
			},
			MonthlyCost: 18.25,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanLoadBalancerRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "delete" {
		t.Fatalf("expected 1 delete rec, got %+v", recs)
	}
}

// TestOrphanLoadBalancerRule_transientStateNotAuto regresses b4-lb-nat-transient:
// LBs whose provisioningState is not "Succeeded" (i.e. Updating, Creating,
// Deleting, Failed) may appear backend-less because they are mid-deploy.
// We still surface a rec so the user sees the resource, but we must
// never mark it AutoExecute — deleting an LB that's currently being
// attached to backends would break a running deployment.
func TestOrphanLoadBalancerRule_transientStateNotAuto(t *testing.T) {
	states := []string{"Updating", "Creating", "Deleting", "Failed"}
	for _, state := range states {
		id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/lb-" + state
		ctx := EvalContext{
			Resources: []providers.Resource{{
				ID:   id,
				Type: "Microsoft.Network/loadBalancers",
				Properties: map[string]interface{}{
					"sku":                 map[string]interface{}{"name": "Standard"},
					"backendAddressPools": []interface{}{},
					"provisioningState":   state,
				},
				MonthlyCost: 18.25,
			}},
			Policies: map[string]*engine.ResolvedPolicy{
				id: {ResourcePolicy: engine.DefaultPolicy()},
			},
		}
		markSafetyKnown(&ctx)
		var r OrphanLoadBalancerRule
		recs := r.Evaluate(ctx)
		if len(recs) != 1 {
			t.Fatalf("state=%q: expected 1 rec, got %d", state, len(recs))
		}
		if recs[0].AutoExecute {
			t.Errorf("state=%q: transient LB must not be AutoExecute", state)
		}
		if recs[0].PolicyNote == "" {
			t.Errorf("state=%q: expected a transient-state PolicyNote", state)
		}
	}
}

func TestOrphanLoadBalancerRule_basicSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/lb2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Network/loadBalancers",
			Properties: map[string]interface{}{
				"sku": map[string]interface{}{"name": "Basic"},
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanLoadBalancerRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("basic LB should be skipped (free), got %d recs", len(recs))
	}
}

func TestOrphanLoadBalancerRule_withBackendConfigsSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/lb3"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Network/loadBalancers",
			Properties: map[string]interface{}{
				"sku": map[string]interface{}{"name": "Standard"},
				"backendAddressPools": []interface{}{
					map[string]interface{}{
						"properties": map[string]interface{}{
							"backendIPConfigurations": []interface{}{map[string]interface{}{"id": "nic"}},
						},
					},
				},
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanLoadBalancerRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("LB with backends should be skipped, got %d", len(recs))
	}
}
