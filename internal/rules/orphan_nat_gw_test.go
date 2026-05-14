package rules

import (
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

func TestOrphanNATGatewayRule_noSubnets(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/natGateways/natgw1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:          id,
			Type:        "Microsoft.Network/natGateways",
			Properties:  map[string]interface{}{},
			MonthlyCost: 32.85,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanNATGatewayRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "delete" {
		t.Fatalf("expected delete rec, got %+v", recs)
	}
}

func TestOrphanNATGatewayRule_withSubnetsSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/natGateways/natgw2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Network/natGateways",
			Properties: map[string]interface{}{
				"subnets": []interface{}{map[string]interface{}{"id": "sub1"}},
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r OrphanNATGatewayRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("attached NAT gw should be skipped, got %d", len(recs))
	}
}
