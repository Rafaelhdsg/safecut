package rules

import (
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

func TestIdleAppServiceRule_stoppedPaidTier_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/site1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Web/sites",
			Properties: map[string]interface{}{
				"sku":   map[string]interface{}{"tier": "Standard"},
				"state": "Stopped",
			},
			MonthlyCost: 55.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleAppServiceRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "downgrade" {
		t.Fatalf("expected downgrade rec, got %+v", recs)
	}
}

func TestIdleAppServiceRule_freeTierSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/site2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Web/sites",
			Properties: map[string]interface{}{
				"sku":   map[string]interface{}{"tier": "Free"},
				"state": "Stopped",
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleAppServiceRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("free tier should be skipped, got %d", len(recs))
	}
}

func TestIdleAppServiceRule_runningStandardAlwaysOnOff(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/site3"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Web/sites",
			Properties: map[string]interface{}{
				"sku":        map[string]interface{}{"tier": "Standard"},
				"state":      "Running",
				"siteConfig": map[string]interface{}{"alwaysOn": false},
			},
			MonthlyCost: 55.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleAppServiceRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "review" {
		t.Fatalf("expected review rec for Standard w/o AlwaysOn, got %+v", recs)
	}
}
