package discovery

import (
	"context"
	"time"

	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// ResourceMetrics holds raw usage metrics for a single cloud resource.
// This is strictly observational data — no interpretation or scoring.
// Scoring and decision logic belong in the engine layer.
type ResourceMetrics struct {
	ResourceID     string
	CPUAvgPercent  float64
	DiskReadBytes  float64
	DiskWriteBytes float64
	NetworkIn      float64
	NetworkOut     float64
	LastActive     time.Time
	IdleDays       int
}

// CollectMetrics gathers raw usage metrics for a resource.
// TODO: integrate with Azure Monitor / CloudWatch for real metric data.
func CollectMetrics(_ context.Context, r *providers.Resource) (*ResourceMetrics, error) {
	return &ResourceMetrics{
		ResourceID: r.ID,
	}, nil
}
