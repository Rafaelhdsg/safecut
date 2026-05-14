package rules

import (
	"strings"
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/discovery"
	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// BUG #3 regression — unknown lock state on an orphan disk must NOT
// auto-execute. Before the fix, any error from
// Microsoft.Authorization/locks/read was silently swallowed, so a disk
// guarded by a CanNotDelete lock the service principal couldn't see
// would still be flagged as "auto-apply safe" for deletion.
func TestApplySafety_unknownLockState_blocksAutoExecute(t *testing.T) {
	disk := providers.Resource{
		ID:            "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d1",
		Name:          "d1",
		Type:          "Microsoft.Compute/disks",
		ResourceGroup: "rg",
		MonthlyCost:   10.0,
		Properties: map[string]interface{}{
			"diskState": "Unattached",
		},
	}

	ctx := EvalContext{
		Resources: []providers.Resource{disk},
		Policies: map[string]*engine.ResolvedPolicy{
			disk.ID: {ResourcePolicy: engine.DefaultPolicy()},
		},
		SafetyProviderSupported: true,
		// Lock probe errored for this resource and its RG.
		LocksStatus: map[string]discovery.SafetyStatus{
			disk.ID: discovery.SafetyDenied,
			"rg":    discovery.SafetyDenied,
		},
		SnapshotsStatus: map[string]discovery.SafetyStatus{
			"rg": discovery.SafetyKnown,
		},
	}

	var r OrphanDiskRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("want 1 rec, got %d", len(recs))
	}
	rec := recs[0]
	if rec.AutoExecute {
		t.Fatalf("AutoExecute must be false when lock state is unknown; rec=%+v", rec)
	}
	if !strings.Contains(strings.ToLower(rec.PolicyNote), "lock state unknown") {
		t.Fatalf("expected policy note about unknown lock state, got %q", rec.PolicyNote)
	}
}

// When the provider does not implement SafetyProvider at all (e.g. a
// future adapter that hasn't added locks support yet), we still fail
// closed.
func TestApplySafety_providerUnsupported_blocksAutoExecute(t *testing.T) {
	disk := providers.Resource{
		ID:            "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d2",
		Name:          "d2",
		Type:          "Microsoft.Compute/disks",
		ResourceGroup: "rg",
		MonthlyCost:   10.0,
		Properties:    map[string]interface{}{"diskState": "Unattached"},
	}
	ctx := EvalContext{
		Resources: []providers.Resource{disk},
		Policies: map[string]*engine.ResolvedPolicy{
			disk.ID: {ResourcePolicy: engine.DefaultPolicy()},
		},
		// SafetyProviderSupported intentionally false — simulates an
		// adapter that doesn't expose locks at all.
	}

	var r OrphanDiskRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("want 1 rec, got %d", len(recs))
	}
	if recs[0].AutoExecute {
		t.Fatalf("AutoExecute must be false when SafetyProvider is unsupported; rec=%+v", recs[0])
	}
}

// Happy path: every probe succeeded and returned no locks → auto-execute allowed.
func TestApplySafety_knownAndEmpty_allowsAutoExecute(t *testing.T) {
	disk := providers.Resource{
		ID:            "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d3",
		Name:          "d3",
		Type:          "Microsoft.Compute/disks",
		ResourceGroup: "rg",
		MonthlyCost:   10.0,
		Properties:    map[string]interface{}{"diskState": "Unattached"},
	}
	ctx := EvalContext{
		Resources: []providers.Resource{disk},
		Policies: map[string]*engine.ResolvedPolicy{
			disk.ID: {ResourcePolicy: engine.DefaultPolicy()},
		},
	}
	markSafetyKnown(&ctx)

	var r OrphanDiskRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("want 1 rec, got %d", len(recs))
	}
	if !recs[0].AutoExecute {
		t.Fatalf("AutoExecute should be true when lock state is known-empty; rec=%+v", recs[0])
	}
}

// Orphan-disk specific: when the *snapshot* probe fails we must also
// refuse auto-delete, even if the lock probe succeeded — we cannot
// confirm the disk is not a snapshot source.
func TestApplySafety_unknownSnapshots_blocksAutoExecute(t *testing.T) {
	disk := providers.Resource{
		ID:            "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d4",
		Name:          "d4",
		Type:          "Microsoft.Compute/disks",
		ResourceGroup: "rg",
		MonthlyCost:   10.0,
		Properties:    map[string]interface{}{"diskState": "Unattached"},
	}
	ctx := EvalContext{
		Resources: []providers.Resource{disk},
		Policies: map[string]*engine.ResolvedPolicy{
			disk.ID: {ResourcePolicy: engine.DefaultPolicy()},
		},
		SafetyProviderSupported: true,
		LocksStatus: map[string]discovery.SafetyStatus{
			disk.ID: discovery.SafetyKnown,
			"rg":    discovery.SafetyKnown,
		},
		SnapshotsStatus: map[string]discovery.SafetyStatus{
			"rg": discovery.SafetyDenied,
		},
	}

	var r OrphanDiskRule
	recs := r.Evaluate(ctx)
	if len(recs) != 1 {
		t.Fatalf("want 1 rec, got %d", len(recs))
	}
	if recs[0].AutoExecute {
		t.Fatalf("AutoExecute must be false when snapshot probe failed; rec=%+v", recs[0])
	}
	if !strings.Contains(strings.ToLower(recs[0].PolicyNote), "snapshot state unknown") {
		t.Fatalf("expected snapshot-unknown note, got %q", recs[0].PolicyNote)
	}
}
