package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// fakeProvider is a minimal Provider + MetricsProvider double so the
// metrics collector can be driven without a real Azure SDK.
type fakeProvider struct {
	name    string
	metrics *providers.ResourceMetrics
	err     error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) ListResources(ctx context.Context, resourceType string) ([]providers.Resource, error) {
	return nil, nil
}
func (f *fakeProvider) GetResource(ctx context.Context, id string) (*providers.Resource, error) {
	return nil, nil
}
func (f *fakeProvider) GetMetrics(ctx context.Context, id, t string) (*providers.ResourceMetrics, error) {
	return f.metrics, f.err
}

// BUG #1 regression: when the metrics API errors out, collectMetrics
// must NOT mask the failure as zero-metrics / idle — it must set
// Status = MetricsDenied and record a reason so the pipeline can skip
// the resource instead of recommending "idle VM, auto-stop safe".
func TestCollectMetrics_apiError_statusDenied(t *testing.T) {
	p := &fakeProvider{err: errors.New("monitor unauthorized")}
	r := &providers.Resource{
		ID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
		Type: "Microsoft.Compute/virtualMachines",
	}

	m := collectMetrics(context.Background(), p, r)

	if m.Status != MetricsDenied {
		t.Fatalf("want Status=MetricsDenied when provider errors, got %q", m.Status)
	}
	if m.StatusReason == "" {
		t.Fatalf("want StatusReason populated, got empty")
	}
	if m.ObservedDays != 0 {
		t.Fatalf("ObservedDays must stay 0 on denied metrics, got %d", m.ObservedDays)
	}
	if !m.LastActive.IsZero() {
		t.Fatalf("LastActive must stay zero on denied metrics, got %v", m.LastActive)
	}
}

// BUG #2 regression: when the provider succeeds but has zero data
// points (diagnostic settings disabled), collectMetrics must NOT
// inflate ObservedDays to the lookback window. The earlier code
// silently produced ObservedDays=14 and a high-confidence "idle"
// verdict on VMs we had never actually monitored.
func TestCollectMetrics_zeroDataPoints_statusDenied(t *testing.T) {
	p := &fakeProvider{metrics: &providers.ResourceMetrics{
		CPUAvgPercent: 0,
		ObservedDays:  0,
	}}
	r := &providers.Resource{
		ID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
		Type: "Microsoft.Compute/virtualMachines",
	}

	m := collectMetrics(context.Background(), p, r)

	if m.ObservedDays != 0 {
		t.Fatalf("ObservedDays must stay 0 when provider reports 0 data points, got %d", m.ObservedDays)
	}
	if m.Status != MetricsDenied {
		t.Fatalf("want Status=MetricsDenied for zero data points, got %q", m.Status)
	}
}

// Happy path: provider returns real data → Status=MetricsKnown and
// ObservedDays is preserved as-is (the real sample count, never padded).
func TestCollectMetrics_knownMetrics(t *testing.T) {
	p := &fakeProvider{metrics: &providers.ResourceMetrics{
		CPUAvgPercent: 42,
		ObservedDays:  7,
	}}
	r := &providers.Resource{
		ID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
		Type: "Microsoft.Compute/virtualMachines",
	}

	m := collectMetrics(context.Background(), p, r)

	if m.Status != MetricsKnown {
		t.Fatalf("want Status=MetricsKnown, got %q", m.Status)
	}
	if m.ObservedDays != 7 {
		t.Fatalf("ObservedDays must mirror the provider's real count, got %d", m.ObservedDays)
	}
	if m.LastActive.IsZero() {
		t.Fatalf("LastActive should be set when ObservedDays>0")
	}
}

// Property-based resource types (disks, public IPs) have no Azure
// Monitor series — the idle signal comes from diskState / ipConfig.
// They must surface as Status=MetricsKnown so property rules can fire.
func TestCollectMetrics_unattachedDisk_known(t *testing.T) {
	p := &fakeProvider{} // no GetMetrics call path uses this, disks skip Monitor

	r := &providers.Resource{
		ID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d1",
		Type: "Microsoft.Compute/disks",
		Properties: map[string]interface{}{
			"diskState": "Unattached",
		},
	}
	m := collectMetrics(context.Background(), p, r)

	if m.Status != MetricsKnown {
		t.Fatalf("disks use property-based detection and should be MetricsKnown, got %q", m.Status)
	}
	if m.IdleDays != 14 {
		t.Fatalf("unattached disks should surface IdleDays=14, got %d", m.IdleDays)
	}
}
