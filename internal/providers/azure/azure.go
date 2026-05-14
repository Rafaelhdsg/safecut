package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// AzureProvider implements Provider, HierarchyProvider, and MetricsProvider
// using Azure Resource Graph, ARM, and Azure Monitor.
type AzureProvider struct {
	subscriptionID string
	cred           azcore.TokenCredential
	pricing        pricing.PricingProvider
}

func New(subscriptionID string) *AzureProvider {
	return &AzureProvider{subscriptionID: subscriptionID}
}

// SetPricing attaches a PricingProvider for real cost lookups.
// When set, resource costs are resolved from the retail pricing API
// instead of hardcoded estimates.
func (a *AzureProvider) SetPricing(p pricing.PricingProvider) {
	a.pricing = p
}

func (a *AzureProvider) ensureAuth() error {
	if a.cred != nil {
		return nil
	}
	cred, err := newCredential()
	if err != nil {
		return err
	}
	a.cred = cred
	return nil
}

func (a *AzureProvider) Name() string { return "azure" }

// ListResources queries Azure Resource Graph for resources of the given type.
// Handles pagination via $skipToken to ensure all resources are captured.
func (a *AzureProvider) ListResources(ctx context.Context, resourceType string) ([]providers.Resource, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armresourcegraph.NewClient(a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("resource graph client: %w", err)
	}

	var query string
	if strings.EqualFold(resourceType, "Microsoft.Compute/virtualMachines") {
		// Use 'extend' to capture power state from instanceView
		query = "Resources " +
			"| where type =~ 'microsoft.compute/virtualmachines' " +
			"| extend powerState = tostring(properties.extended.instanceView.powerState.code) " +
			"| project id, name, type, resourceGroup, location, tags, properties, sku, powerState"
	} else {
		query = fmt.Sprintf(
			"Resources | where type =~ '%s' | project id, name, type, resourceGroup, location, tags, properties, sku",
			strings.ToLower(resourceType),
		)
	}

	var allResources []providers.Resource
	var skipToken *string

	for {
		req := armresourcegraph.QueryRequest{
			Query:         to.Ptr(query),
			Subscriptions: []*string{to.Ptr(a.subscriptionID)},
			Options: &armresourcegraph.QueryRequestOptions{
				Top: to.Ptr[int32](1000),
			},
		}
		if skipToken != nil {
			req.Options.SkipToken = skipToken
		}

		resp, err := client.Resources(ctx, req, nil)
		if err != nil {
			return nil, fmt.Errorf("resource graph query for %s: %w", resourceType, err)
		}

		page, err := a.parseResourceGraphResponse(resp, resourceType)
		if err != nil {
			return nil, err
		}
		allResources = append(allResources, page...)

		if resp.SkipToken == nil || *resp.SkipToken == "" {
			break
		}
		skipToken = resp.SkipToken
	}

	return allResources, nil
}

func (a *AzureProvider) parseResourceGraphResponse(resp armresourcegraph.ClientResourcesResponse, resourceType string) ([]providers.Resource, error) {
	if resp.Data == nil {
		// A nil payload with no error is Azure's way of saying "no rows
		// matched". This is legitimate and must not be conflated with
		// a malformed payload.
		return nil, nil
	}
	data, ok := resp.Data.([]interface{})
	if !ok {
		// Returning (nil, nil) here used to make the scanner look like
		// it succeeded with zero rows, which silently hid a broken
		// query or an SDK contract change. A surprise "0 resources"
		// is the single worst UX — fail loudly instead.
		return nil, fmt.Errorf(
			"resource graph returned unexpected payload type %T for %s (expected []interface{})",
			resp.Data, resourceType,
		)
	}

	ctx := context.Background()
	var resources []providers.Resource
	for _, item := range data {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		props, err := parseMap(row["properties"])
		if err != nil {
			// Skipping is the honest response: a resource whose
			// properties we can't parse may look "OK" with default
			// zero values but any subsequent cost or idle decision
			// on it would be wrong. Better one fewer row in the
			// scan than a false recommendation.
			return nil, fmt.Errorf("resource graph row %s: properties: %w",
				strVal(row, "id"), err)
		}
		skuMap, err := parseMap(row["sku"])
		if err != nil {
			return nil, fmt.Errorf("resource graph row %s: sku: %w",
				strVal(row, "id"), err)
		}

		r := providers.Resource{
			ID:            strVal(row, "id"),
			Name:          strVal(row, "name"),
			Type:          strVal(row, "type"),
			ResourceGroup: strVal(row, "resourceGroup"),
			Location:      strVal(row, "location"),
			Tags:          parseTags(row["tags"]),
			Properties:    props,
		}

		price := a.resolvePrice(ctx, r.Type, r.Location, r.Properties, skuMap)
		r.MonthlyCost = price.cost
		r.PriceFallback = price.fallback

		// Power state: try top-level projected field first, then properties
		if ps := strVal(row, "powerState"); ps != "" {
			r.PowerState = normalizePowerState(ps)
		} else {
			r.PowerState = extractPowerState(r.Properties)
		}

		resources = append(resources, r)
	}
	return resources, nil
}

type priceResult struct {
	cost     float64
	fallback bool
}

// resolvePrice asks the Retail Prices API for the authoritative
// monthly cost. If either the API call fails or no row matches the
// SKU we return (0, fallback=true) — never a hardcoded guess.
// Consumers (rules) treat MonthlyCost<=0 as "price unavailable" and
// exclude the resource from TotalSaving aggregation, which keeps the
// headline number honest even when pricing is partially unreachable.
func (a *AzureProvider) resolvePrice(ctx context.Context, resourceType, location string, properties, sku map[string]interface{}) priceResult {
	if a.pricing == nil {
		return priceResult{cost: 0, fallback: true}
	}

	lower := strings.ToLower(resourceType)
	switch {
	case strings.Contains(lower, "virtualmachines"):
		vmSize := extractNestedString(properties, "hardwareProfile", "vmSize")
		if vmSize != "" {
			if price, err := a.pricing.GetVMPrice(ctx, vmSize, location); err == nil && price > 0 {
				return priceResult{cost: price}
			}
		}
	case strings.Contains(lower, "disks"):
		skuName := ""
		if v, ok := sku["name"].(string); ok {
			skuName = v
		}
		sizeGB := 0.0
		if v, ok := properties["diskSizeGB"].(float64); ok {
			sizeGB = v
		}
		if skuName != "" {
			if price, err := a.pricing.GetDiskPrice(ctx, skuName, sizeGB, location); err == nil && price > 0 {
				return priceResult{cost: price}
			}
		}
	case strings.Contains(lower, "publicipaddresses"):
		skuName := ""
		if v, ok := sku["name"].(string); ok {
			skuName = v
		}
		if price, err := a.pricing.GetIPPrice(ctx, skuName, location); err == nil && price > 0 {
			return priceResult{cost: price}
		}
	case strings.Contains(lower, "microsoft.web/sites"):
		planSKU := ""
		if v, ok := sku["name"].(string); ok {
			planSKU = v
		}
		if planSKU == "" {
			planSKU = extractNestedString(properties, "siteConfig", "appServicePlanId")
		}
		isLinux := false
		if kind, ok := properties["kind"].(string); ok {
			isLinux = strings.Contains(strings.ToLower(kind), "linux")
		}
		if planSKU != "" {
			if price, err := a.pricing.GetAppServicePrice(ctx, planSKU, isLinux, location); err == nil && price > 0 {
				return priceResult{cost: price}
			}
		}
	case strings.Contains(lower, "microsoft.sql/servers/databases"):
		tier := ""
		if v, ok := sku["tier"].(string); ok {
			tier = v
		}
		if tier == "" {
			if v, ok := sku["name"].(string); ok {
				tier = v
			}
		}
		if tier != "" {
			if price, err := a.pricing.GetSQLDBPrice(ctx, tier, location); err == nil && price > 0 {
				return priceResult{cost: price}
			}
		}
	case strings.Contains(lower, "loadbalancers"):
		skuTier := "Standard"
		if v, ok := sku["name"].(string); ok {
			skuTier = v
		}
		if price, err := a.pricing.GetLoadBalancerPrice(ctx, skuTier, location); err == nil && price > 0 {
			return priceResult{cost: price}
		}
	case strings.Contains(lower, "natgateways"):
		if price, err := a.pricing.GetNATGatewayPrice(ctx, location); err == nil && price > 0 {
			return priceResult{cost: price}
		}
	case strings.Contains(lower, "containergroups"):
		cpuCores, memoryGB := extractContainerResources(properties)
		if cpuCores > 0 || memoryGB > 0 {
			if price, err := a.pricing.GetContainerGroupPrice(ctx, cpuCores, memoryGB, location); err == nil && price > 0 {
				return priceResult{cost: price}
			}
		}
	}

	return priceResult{cost: 0, fallback: true}
}

// GetResource fetches a single resource by ID via ARM.
func (a *AzureProvider) GetResource(ctx context.Context, resourceID string) (*providers.Resource, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armresources.NewClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("arm resources client: %w", err)
	}

	apiVersion := inferAPIVersion(resourceID)
	resp, err := client.GetByID(ctx, resourceID, apiVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("get resource %s: %w", resourceID, err)
	}

	r := &providers.Resource{
		ID:       ptrStr(resp.ID),
		Name:     ptrStr(resp.Name),
		Type:     ptrStr(resp.Type),
		Location: ptrStr(resp.Location),
	}
	if resp.Tags != nil {
		r.Tags = make(map[string]string, len(resp.Tags))
		for k, v := range resp.Tags {
			if v != nil {
				r.Tags[k] = *v
			}
		}
	}
	return r, nil
}

// GetSubscriptionTags returns tags on the subscription scope.
func (a *AzureProvider) GetSubscriptionTags(ctx context.Context) (map[string]string, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armsubscriptions.NewClient(a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("subscriptions client: %w", err)
	}

	resp, err := client.Get(ctx, a.subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("get subscription %s: %w", a.subscriptionID, err)
	}

	result := make(map[string]string)
	if resp.Tags != nil {
		for k, v := range resp.Tags {
			if v != nil {
				result[k] = *v
			}
		}
	}
	return result, nil
}

// GetSubscriptionName returns the subscription's human-readable
// display name. The rest of the pipeline reads this onto
// Snapshot.SubscriptionName; without it, drift and lint outputs show
// a bare GUID that is nearly useless to a human reviewer.
func (a *AzureProvider) GetSubscriptionName(ctx context.Context) (string, error) {
	if err := a.ensureAuth(); err != nil {
		return "", err
	}

	client, err := armsubscriptions.NewClient(a.cred, nil)
	if err != nil {
		return "", fmt.Errorf("subscriptions client: %w", err)
	}
	resp, err := client.Get(ctx, a.subscriptionID, nil)
	if err != nil {
		return "", fmt.Errorf("get subscription %s: %w", a.subscriptionID, err)
	}
	if resp.DisplayName == nil {
		return "", nil
	}
	return *resp.DisplayName, nil
}

// GetResourceGroupTags returns tags on a specific resource group.
func (a *AzureProvider) GetResourceGroupTags(ctx context.Context, resourceGroup string) (map[string]string, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armresources.NewResourceGroupsClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("resource groups client: %w", err)
	}

	resp, err := client.Get(ctx, resourceGroup, nil)
	if err != nil {
		return nil, fmt.Errorf("get resource group %s: %w", resourceGroup, err)
	}

	result := make(map[string]string)
	if resp.Tags != nil {
		for k, v := range resp.Tags {
			if v != nil {
				result[k] = *v
			}
		}
	}
	return result, nil
}

// GetMetrics fetches usage metrics from Azure Monitor.
func (a *AzureProvider) GetMetrics(ctx context.Context, resourceID string, resourceType string) (*providers.ResourceMetrics, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	return queryMonitorMetrics(ctx, a.cred, resourceID, resourceType)
}

// ── helpers ──

func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseTags(v interface{}) map[string]string {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	tags := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			tags[k] = s
		}
	}
	return tags
}

// parseMap normalises an untyped Resource Graph field into a
// map[string]interface{}. It returns an error on *any* step that
// could silently swallow structure, so a malformed properties blob
// doesn't become "nil properties, zero cost" downstream. Callers
// that treat a missing field as OK (fields are legitimately absent)
// pass the result through parseMapLoose instead.
func parseMap(v interface{}) (map[string]interface{}, error) {
	if v == nil {
		return nil, nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal property blob: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal property blob: %w", err)
	}
	return m, nil
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractContainerResources sums the CPU and memory across all containers
// in a container group's properties.
func extractContainerResources(props map[string]interface{}) (cpuCores, memoryGB float64) {
	containers, _ := props["containers"].([]interface{})
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cprops, _ := cm["properties"].(map[string]interface{})
		if cprops == nil {
			cprops = cm
		}
		res, _ := cprops["resources"].(map[string]interface{})
		if res == nil {
			continue
		}
		req, _ := res["requests"].(map[string]interface{})
		if req == nil {
			continue
		}
		if v, ok := req["cpu"].(float64); ok {
			cpuCores += v
		}
		if v, ok := req["memoryInGB"].(float64); ok {
			memoryGB += v
		}
	}
	if cpuCores == 0 {
		cpuCores = 1
	}
	if memoryGB == 0 {
		memoryGB = 1.5
	}
	return
}

func inferAPIVersion(resourceID string) string {
	lower := strings.ToLower(resourceID)
	switch {
	case strings.Contains(lower, "microsoft.compute/virtualmachines"):
		return "2024-07-01"
	case strings.Contains(lower, "microsoft.compute/disks"):
		return "2024-03-02"
	case strings.Contains(lower, "microsoft.network/publicipaddresses"):
		return "2024-05-01"
	case strings.Contains(lower, "microsoft.network/networkinterfaces"):
		return "2024-05-01"
	case strings.Contains(lower, "microsoft.web/sites"):
		return "2024-04-01"
	case strings.Contains(lower, "microsoft.sql/servers/databases"):
		return "2024-05-01-preview"
	case strings.Contains(lower, "microsoft.storage/storageaccounts"):
		return "2023-05-01"
	case strings.Contains(lower, "microsoft.network/loadbalancers"):
		return "2024-05-01"
	case strings.Contains(lower, "microsoft.network/natgateways"):
		return "2024-05-01"
	case strings.Contains(lower, "microsoft.containerinstance/containergroups"):
		return "2024-05-01-preview"
	default:
		return "2024-01-01"
	}
}
