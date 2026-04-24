package providers

import "context"

// Resource is a generic representation of a cloud resource.
type Resource struct {
	ID            string
	Name          string
	Type          string
	ResourceGroup string
	Location      string
	Tags          map[string]string
	Properties    map[string]interface{}
	MonthlyCost   float64
	PriceFallback bool   // true when cost came from hardcoded estimate, not real API
	PowerState    string // "running", "deallocated", "stopped", "" (unknown/N/A)
}

// LockInfo describes a management lock on a resource or its scope.
type LockInfo struct {
	Level string // "CanNotDelete", "ReadOnly"
	Scope string // "resource", "resourceGroup", "subscription"
	Notes string
}

// SnapshotInfo describes a snapshot referencing a disk.
type SnapshotInfo struct {
	ID   string
	Name string
}

// Provider defines the interface that every cloud provider adapter must implement.
type Provider interface {
	Name() string
	ListResources(ctx context.Context, resourceType string) ([]Resource, error)
	GetResource(ctx context.Context, resourceID string) (*Resource, error)
}

// HierarchyProvider extends Provider with methods to read tags from
// parent scopes (subscription, resource group) and to look up the
// human-readable subscription display name. Policy output (drift,
// lint summary) is significantly clearer when the subscription name
// is populated alongside the ID.
type HierarchyProvider interface {
	Provider
	GetSubscriptionTags(ctx context.Context) (map[string]string, error)
	GetResourceGroupTags(ctx context.Context, resourceGroup string) (map[string]string, error)
	GetSubscriptionName(ctx context.Context) (string, error)
}

// MetricsProvider extends Provider with the ability to fetch usage
// metrics for a resource from the cloud's monitoring service.
type MetricsProvider interface {
	Provider
	GetMetrics(ctx context.Context, resourceID string, resourceType string) (*ResourceMetrics, error)
}

// SafetyProvider extends Provider with methods to check operational
// safety constraints (locks, snapshots) before recommending actions.
type SafetyProvider interface {
	Provider
	ListResourceLocks(ctx context.Context, resourceID string) ([]LockInfo, error)
	ListResourceGroupLocks(ctx context.Context, resourceGroup string) ([]LockInfo, error)
	ListDiskSnapshots(ctx context.Context, resourceGroup string) ([]SnapshotInfo, error)
}

// ResourceMetrics holds raw usage metrics returned by a MetricsProvider.
//
// ObservedDays reflects the *real* number of daily data points the
// provider saw. It is NEVER inflated to the lookback window — a VM
// with diagnostic settings disabled, or one whose metrics query was
// rate-limited, must surface ObservedDays == 0 so downstream
// consumers can distinguish "genuinely idle" from "we have no idea".
type ResourceMetrics struct {
	CPUAvgPercent  float64
	DiskReadBytes  float64
	DiskWriteBytes float64
	NetworkIn      float64
	NetworkOut     float64
	IdleDays       int
	ObservedDays   int
}
