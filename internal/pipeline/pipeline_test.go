package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// fakeProvider implements Provider, MetricsProvider, HierarchyProvider, and SafetyProvider
// with in-memory data only (no Azure calls).
type fakeProvider struct {
	byType map[string][]providers.Resource
	// Optional injection points for regression tests:
	metricsErr error                      // BUG #1: simulate Azure Monitor failure
	metrics    *providers.ResourceMetrics // BUG #2: override the default metrics payload
	lockErr    error                      // BUG #3: simulate locks/read 403
	locks      map[string][]providers.LockInfo
	// v1.0 additions:
	subTagsErr error // simulate subscription-tag 403
	rgTagsErr  error // simulate rg-tag 403
	subName    string
	subNameErr error
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ListResources(ctx context.Context, resourceType string) ([]providers.Resource, error) {
	if f.byType == nil {
		return nil, nil
	}
	return f.byType[resourceType], nil
}

func (f *fakeProvider) GetResource(ctx context.Context, resourceID string) (*providers.Resource, error) {
	return nil, nil
}

func (f *fakeProvider) GetMetrics(ctx context.Context, resourceID string, resourceType string) (*providers.ResourceMetrics, error) {
	if f.metricsErr != nil {
		return nil, f.metricsErr
	}
	if f.metrics != nil {
		return f.metrics, nil
	}
	return &providers.ResourceMetrics{CPUAvgPercent: 0.1, ObservedDays: 14}, nil
}

func (f *fakeProvider) GetSubscriptionTags(ctx context.Context) (map[string]string, error) {
	if f.subTagsErr != nil {
		return nil, f.subTagsErr
	}
	return map[string]string{}, nil
}

func (f *fakeProvider) GetResourceGroupTags(ctx context.Context, resourceGroup string) (map[string]string, error) {
	if f.rgTagsErr != nil {
		return nil, f.rgTagsErr
	}
	return map[string]string{}, nil
}

func (f *fakeProvider) GetSubscriptionName(ctx context.Context) (string, error) {
	if f.subNameErr != nil {
		return "", f.subNameErr
	}
	if f.subName != "" {
		return f.subName, nil
	}
	return "fake-subscription", nil
}

func (f *fakeProvider) ListResourceLocks(ctx context.Context, resourceID string) ([]providers.LockInfo, error) {
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	if f.locks != nil {
		return f.locks[resourceID], nil
	}
	return nil, nil
}

func (f *fakeProvider) ListResourceGroupLocks(ctx context.Context, resourceGroup string) ([]providers.LockInfo, error) {
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	if f.locks != nil {
		return f.locks[resourceGroup], nil
	}
	return nil, nil
}

func (f *fakeProvider) ListDiskSnapshots(ctx context.Context, resourceGroup string) ([]providers.SnapshotInfo, error) {
	return nil, nil
}

func TestPipeline_Run_orphanDiskRecommendation(t *testing.T) {
	ctx := context.Background()
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/orphan-disk"
	fp := &fakeProvider{
		byType: map[string][]providers.Resource{
			"Microsoft.Compute/disks": {{
				ID:            diskID,
				Name:          "orphan-disk",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Location:      "eastus",
				Properties:    map[string]interface{}{"diskState": "Unattached"},
				MonthlyCost:   10.0,
			}},
		},
	}
	p := New(fp)
	out, err := p.Run(ctx, []string{"Microsoft.Compute/disks"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range out.Decisions {
		if strings.Contains(r.ResourceID, "orphan-disk") && r.Action == "delete" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected orphan disk delete recommendation among %d decisions", len(out.Decisions))
	}
}

// BUG #1 regression (pipeline-level): when Azure Monitor errors out
// for a VM, the pipeline must NOT produce any "idle VM" recommendation
// for that resource. The old behaviour silently zeroed the metrics
// and happily flagged every un-monitorable VM as idle.
func TestPipeline_Run_metricsError_skipsIdleRec(t *testing.T) {
	ctx := context.Background()
	vmID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-err"
	fp := &fakeProvider{
		byType: map[string][]providers.Resource{
			"Microsoft.Compute/virtualMachines": {{
				ID:            vmID,
				Name:          "vm-err",
				Type:          "Microsoft.Compute/virtualMachines",
				ResourceGroup: "rg",
				Location:      "eastus",
				PowerState:    "running",
				MonthlyCost:   100.0,
			}},
		},
		metricsErr: errors.New("monitor unauthorized"),
	}
	p := New(fp)
	out, err := p.Run(ctx, []string{"Microsoft.Compute/virtualMachines"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Decisions {
		if r.ResourceID == vmID {
			t.Fatalf("VM with denied metrics must not produce recommendations, got %+v", r)
		}
	}
}

// BUG #2 regression (pipeline-level): zero data points from the
// provider must be treated as MetricsDenied, so the pipeline skips
// the resource instead of calling it "idle with confidence 1.0".
func TestPipeline_Run_zeroDataPoints_skipsIdleRec(t *testing.T) {
	ctx := context.Background()
	vmID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-zero"
	fp := &fakeProvider{
		byType: map[string][]providers.Resource{
			"Microsoft.Compute/virtualMachines": {{
				ID:            vmID,
				Name:          "vm-zero",
				Type:          "Microsoft.Compute/virtualMachines",
				ResourceGroup: "rg",
				Location:      "eastus",
				PowerState:    "running",
				MonthlyCost:   100.0,
			}},
		},
		// provider answers with zero observed days — diagnostic settings off
		metrics: &providers.ResourceMetrics{CPUAvgPercent: 0, ObservedDays: 0},
	}
	p := New(fp)
	out, err := p.Run(ctx, []string{"Microsoft.Compute/virtualMachines"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Decisions {
		if r.ResourceID == vmID {
			t.Fatalf("VM with 0 observed days must not produce recommendations, got %+v", r)
		}
	}
}

// TestPipeline_Run_subNameAndTagWarnings regresses two v1.0 fixes:
//  1. SubscriptionName is populated via the hierarchy provider's
//     GetSubscriptionName method (not just the raw ID).
//  2. Failures in the subscription-tag or RG-tag probe are appended to
//     Snapshot.TagsWarnings, not silently dropped. The old collector
//     swallowed both errors, so operators never knew why their policy
//     inheritance wasn't kicking in.
func TestPipeline_Run_subNameAndTagWarnings(t *testing.T) {
	ctx := context.Background()
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d1"
	fp := &fakeProvider{
		byType: map[string][]providers.Resource{
			"Microsoft.Compute/disks": {{
				ID:            diskID,
				Name:          "d1",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Properties:    map[string]interface{}{"diskState": "Unattached"},
			}},
		},
		subName:    "prod-subscription",
		subTagsErr: errors.New("subscription tag read forbidden"),
	}
	p := New(fp)
	out, err := p.Run(ctx, []string{"Microsoft.Compute/disks"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Snapshot.SubscriptionName != "prod-subscription" {
		t.Errorf("SubscriptionName: got %q, want %q",
			out.Snapshot.SubscriptionName, "prod-subscription")
	}
	if len(out.TagsWarnings) == 0 {
		t.Fatal("expected TagsWarnings to capture subscription-tag probe error")
	}
	var sawSubWarning bool
	for _, w := range out.TagsWarnings {
		if strings.Contains(w, "subscription tag") {
			sawSubWarning = true
			break
		}
	}
	if !sawSubWarning {
		t.Errorf("TagsWarnings missing subscription-tag entry, got %v", out.TagsWarnings)
	}
}

// BUG #3 regression (pipeline-level): when locks/read returns 403,
// the pipeline still emits recommendations — but every one of them
// must be AutoExecute=false and carry the "lock state unknown" note.
// Previously, the collector silently discarded the error and the
// rule assumed "no lock present".
func TestPipeline_Run_lockProbeDenied_downgradesAutoExecute(t *testing.T) {
	ctx := context.Background()
	diskID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d-denied"
	fp := &fakeProvider{
		byType: map[string][]providers.Resource{
			"Microsoft.Compute/disks": {{
				ID:            diskID,
				Name:          "d-denied",
				Type:          "Microsoft.Compute/disks",
				ResourceGroup: "rg",
				Location:      "eastus",
				Properties:    map[string]interface{}{"diskState": "Unattached"},
				MonthlyCost:   10.0,
			}},
		},
		lockErr: errors.New("locks/read forbidden"),
	}
	p := New(fp)
	out, err := p.Run(ctx, []string{"Microsoft.Compute/disks"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var diskRec *struct {
		AutoExecute bool
		PolicyNote  string
	}
	for _, r := range out.Decisions {
		if r.ResourceID == diskID {
			diskRec = &struct {
				AutoExecute bool
				PolicyNote  string
			}{AutoExecute: r.AutoExecute, PolicyNote: r.PolicyNote}
			break
		}
	}
	if diskRec == nil {
		t.Fatalf("expected a decision for the orphan disk, got %d", len(out.Decisions))
	}
	if diskRec.AutoExecute {
		t.Fatalf("AutoExecute must be false when lock probe failed, got note=%q", diskRec.PolicyNote)
	}
	if !strings.Contains(strings.ToLower(diskRec.PolicyNote), "lock state unknown") {
		t.Fatalf("expected lock-state-unknown note, got %q", diskRec.PolicyNote)
	}
}
