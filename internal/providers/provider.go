package providers

import "context"

// Resource is a generic representation of a cloud resource.
type Resource struct {
	ID             string
	Name           string
	Type           string
	ResourceGroup  string
	Location       string
	Tags           map[string]string
	Properties     map[string]interface{}
	MonthlyCost    float64
}

// Provider defines the interface that every cloud provider adapter must implement.
// This abstraction enables future multi-cloud support (Azure, AWS, GCP).
type Provider interface {
	Name() string
	ListResources(ctx context.Context, resourceType string) ([]Resource, error)
	GetResource(ctx context.Context, resourceID string) (*Resource, error)
}
