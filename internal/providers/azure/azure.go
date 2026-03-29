package azure

import (
	"context"
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// AzureProvider implements Provider and HierarchyProvider using Azure Resource Graph.
type AzureProvider struct {
	subscriptionID string
}

func New(subscriptionID string) *AzureProvider {
	return &AzureProvider{subscriptionID: subscriptionID}
}

func (a *AzureProvider) Name() string {
	return "azure"
}

func (a *AzureProvider) ListResources(ctx context.Context, resourceType string) ([]providers.Resource, error) {
	// TODO: implement Azure Resource Graph query
	return nil, fmt.Errorf("azure: ListResources not yet implemented")
}

func (a *AzureProvider) GetResource(ctx context.Context, resourceID string) (*providers.Resource, error) {
	// TODO: implement single resource lookup via ARM
	return nil, fmt.Errorf("azure: GetResource not yet implemented")
}

func (a *AzureProvider) GetSubscriptionTags(ctx context.Context) (map[string]string, error) {
	// TODO: implement via ARM subscription API
	return nil, fmt.Errorf("azure: GetSubscriptionTags not yet implemented")
}

func (a *AzureProvider) GetResourceGroupTags(ctx context.Context, resourceGroup string) (map[string]string, error) {
	// TODO: implement via ARM resource group API
	return nil, fmt.Errorf("azure: GetResourceGroupTags not yet implemented")
}
