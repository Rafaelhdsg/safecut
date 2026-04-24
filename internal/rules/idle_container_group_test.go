package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestIdleContainerGroupRule_stoppedFlagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/cg1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.ContainerInstance/containerGroups",
			Properties: map[string]interface{}{
				"instanceView": map[string]interface{}{"state": "Stopped"},
			},
			MonthlyCost: 50.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleContainerGroupRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "delete" {
		t.Fatalf("expected delete rec for stopped CG, got %+v", recs)
	}
}

func TestIdleContainerGroupRule_provisioningFailed(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/cg2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.ContainerInstance/containerGroups",
			Properties: map[string]interface{}{
				"provisioningState": "Failed",
			},
			MonthlyCost: 10.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleContainerGroupRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("expected rec for failed CG, got %+v", recs)
	}
}

func TestIdleContainerGroupRule_runningSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/cg3"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.ContainerInstance/containerGroups",
			Properties: map[string]interface{}{
				"instanceView": map[string]interface{}{"state": "Running"},
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleContainerGroupRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("running CG should be skipped, got %d", len(recs))
	}
}
