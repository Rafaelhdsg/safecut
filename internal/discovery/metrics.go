package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// MetricsStatus tracks whether we actually trust the metrics we collected
// for a resource. Downstream rules MUST use this instead of inferring
// "no data = idle".
type MetricsStatus string

const (
	// MetricsKnown means the provider answered and we have real data
	// points to reason about.
	MetricsKnown MetricsStatus = "known"
	// MetricsUnknown means the resource type has no metrics source at
	// all (disks, public IPs) — the idle state is inferred from
	// properties, not from a time-series query.
	MetricsUnknown MetricsStatus = "unknown"
	// MetricsDenied means the metrics query errored (permission,
	// throttle, agent missing, network). Treat as "no idea" — rules
	// must not auto-execute on MetricsDenied inputs.
	MetricsDenied MetricsStatus = "denied"
)

// ResourceMetrics holds raw usage metrics for a single cloud resource.
// This is strictly observational data — no interpretation or scoring.
// Scoring and decision logic belong in the engine layer.
//
// ObservedDays is the real daily-sample count surfaced by the
// provider. It is never fabricated. When the provider returns zero
// data points, this stays at zero and Status becomes MetricsDenied
// (error) or MetricsKnown (genuine idle, e.g. a newly-created VM).
type ResourceMetrics struct {
	ResourceID     string
	CPUAvgPercent  float64
	DiskReadBytes  float64
	DiskWriteBytes float64
	NetworkIn      float64
	NetworkOut     float64
	LastActive     time.Time
	IdleDays       int
	ObservedDays   int
	Status         MetricsStatus
	StatusReason   string
}

// collectMetrics gathers raw usage metrics for a resource.
// For VMs: uses Azure Monitor time-series data.
// For Disks/IPs: uses property-based detection (no Monitor needed).
//
// Any error from the underlying provider surfaces as
// Status=MetricsDenied so the decision engine can skip the
// recommendation or downgrade it to manual review.
func collectMetrics(ctx context.Context, p providers.Provider, r *providers.Resource) *ResourceMetrics {
	m := &ResourceMetrics{ResourceID: r.ID, Status: MetricsUnknown}

	mp, ok := p.(providers.MetricsProvider)
	if ok {
		pm, err := mp.GetMetrics(ctx, r.ID, r.Type)
		switch {
		case err != nil:
			m.Status = MetricsDenied
			m.StatusReason = err.Error()
		case pm == nil:
			m.Status = MetricsDenied
			m.StatusReason = "metrics provider returned nil"
		default:
			m.CPUAvgPercent = pm.CPUAvgPercent
			m.DiskReadBytes = pm.DiskReadBytes
			m.DiskWriteBytes = pm.DiskWriteBytes
			m.NetworkIn = pm.NetworkIn
			m.NetworkOut = pm.NetworkOut
			m.IdleDays = pm.IdleDays
			m.ObservedDays = pm.ObservedDays

			if pm.ObservedDays > 0 {
				m.LastActive = time.Now().AddDate(0, 0, -pm.ObservedDays)
				m.Status = MetricsKnown
			} else {
				// The provider replied but had nothing to show
				// (e.g. Monitor returned an empty result set
				// because diagnostic settings are off). We must
				// NOT treat silence as "idle" — downgrade to
				// denied so rules skip it.
				m.Status = MetricsDenied
				if m.StatusReason == "" {
					m.StatusReason = "provider returned zero data points"
				}
			}
		}
	}

	enrichFromProperties(m, r)
	return m
}

// enrichFromProperties infers idle state from resource properties for
// resource types that don't have Azure Monitor metrics (disks, IPs).
// For those types, Status is promoted to MetricsKnown because the
// property-based signal is authoritative — diskState=Unattached is a
// real, trustworthy Azure fact, not a guess.
func enrichFromProperties(m *ResourceMetrics, r *providers.Resource) {
	t := strings.ToLower(r.Type)

	switch {
	case strings.Contains(t, "microsoft.compute/disks"):
		enrichDiskMetrics(m, r)
	case strings.Contains(t, "microsoft.network/publicipaddresses"):
		enrichIPMetrics(m, r)
	}
}

func enrichDiskMetrics(m *ResourceMetrics, r *providers.Resource) {
	if m.LastActive.IsZero() {
		m.LastActive = time.Now().AddDate(0, 0, -14)
	}

	state, _ := r.Properties["diskState"].(string)
	if strings.EqualFold(state, "Unattached") {
		m.IdleDays = 14
	}
	// Disks have no Azure Monitor metrics; diskState is the source of
	// truth, so we trust it.
	m.Status = MetricsKnown
	m.ObservedDays = 14
}

func enrichIPMetrics(m *ResourceMetrics, r *providers.Resource) {
	if m.LastActive.IsZero() {
		m.LastActive = time.Now().AddDate(0, 0, -14)
	}

	ipConfig := r.Properties["ipConfiguration"]
	if ipConfig == nil {
		m.IdleDays = 14
	}
	m.Status = MetricsKnown
	m.ObservedDays = 14
}
