package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestIdleStorageAccountRule_noTracking_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:          id,
			Type:        "Microsoft.Storage/storageAccounts",
			Properties:  map[string]interface{}{},
			MonthlyCost: 20.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleStorageAccountRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "review" {
		t.Fatalf("expected review rec, got %+v", recs)
	}
}

func TestIdleStorageAccountRule_withTrackingSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Storage/storageAccounts",
			Properties: map[string]interface{}{
				"lastAccessTimeTrackingPolicy": map[string]interface{}{"enable": true},
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleStorageAccountRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("tracked storage should be skipped, got %d", len(recs))
	}
}
