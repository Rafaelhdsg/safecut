package rules

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

func TestIdleSQLDatabaseRule_pausedPaidTier_flagged(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Sql/servers/sv/databases/db1"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Sql/servers/databases",
			Properties: map[string]interface{}{
				"sku":    map[string]interface{}{"tier": "Standard"},
				"status": "Paused",
			},
			MonthlyCost: 200.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleSQLDatabaseRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Action != "review" {
		t.Fatalf("expected review rec, got %+v", recs)
	}
}

func TestIdleSQLDatabaseRule_freeTierSkipped(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Sql/servers/sv/databases/db2"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Sql/servers/databases",
			Properties: map[string]interface{}{
				"sku":    map[string]interface{}{"tier": "Free"},
				"status": "Paused",
			},
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	var r IdleSQLDatabaseRule
	if recs := r.Evaluate(ctx); len(recs) != 0 {
		t.Fatalf("free tier should be skipped, got %d", len(recs))
	}
}

func TestIdleSQLDatabaseRule_premiumOnline_flaggedReview(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Sql/servers/sv/databases/db3"
	ctx := EvalContext{
		Resources: []providers.Resource{{
			ID:   id,
			Type: "Microsoft.Sql/servers/databases",
			Properties: map[string]interface{}{
				"sku":    map[string]interface{}{"tier": "Premium"},
				"status": "Online",
			},
			MonthlyCost: 1500.0,
		}},
		Policies: map[string]*engine.ResolvedPolicy{
			id: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	markSafetyKnown(&ctx)
	var r IdleSQLDatabaseRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 || recs[0].Risk != engine.RiskMedium {
		t.Fatalf("expected medium-risk review rec for Premium online, got %+v", recs)
	}
}
