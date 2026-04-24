package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armlocks"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// extractPowerState reads the VM power state from resource properties.
// Resource Graph returns this in properties.extended.instanceView.powerState.code
// or properties.extended.instanceView.statuses[].code.
func extractPowerState(props map[string]interface{}) string {
	if props == nil {
		return ""
	}

	// Try: properties.extended.instanceView.powerState.code
	extended, ok := props["extended"].(map[string]interface{})
	if !ok {
		return inferPowerStateFromStatuses(props)
	}
	iv, ok := extended["instanceView"].(map[string]interface{})
	if !ok {
		return inferPowerStateFromStatuses(props)
	}

	// Direct powerState field
	if ps, ok := iv["powerState"].(map[string]interface{}); ok {
		if code, ok := ps["code"].(string); ok {
			return normalizePowerState(code)
		}
	}

	// Statuses array fallback
	if statuses, ok := iv["statuses"].([]interface{}); ok {
		for _, s := range statuses {
			if status, ok := s.(map[string]interface{}); ok {
				code, _ := status["code"].(string)
				if strings.HasPrefix(code, "PowerState/") {
					return normalizePowerState(code)
				}
			}
		}
	}

	return ""
}

func inferPowerStateFromStatuses(props map[string]interface{}) string {
	// Some queries return statuses at properties.instanceView.statuses
	iv, ok := props["instanceView"].(map[string]interface{})
	if !ok {
		return ""
	}
	statuses, ok := iv["statuses"].([]interface{})
	if !ok {
		return ""
	}
	for _, s := range statuses {
		if status, ok := s.(map[string]interface{}); ok {
			code, _ := status["code"].(string)
			if strings.HasPrefix(code, "PowerState/") {
				return normalizePowerState(code)
			}
		}
	}
	return ""
}

func normalizePowerState(code string) string {
	code = strings.ToLower(code)
	code = strings.TrimPrefix(code, "powerstate/")
	switch code {
	case "running":
		return "running"
	case "deallocated":
		return "deallocated"
	case "stopped":
		return "stopped"
	case "deallocating":
		return "deallocating"
	case "starting":
		return "starting"
	default:
		return code
	}
}

// ListResourceLocks returns management locks on a specific resource.
func (a *AzureProvider) ListResourceLocks(ctx context.Context, resourceID string) ([]providers.LockInfo, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armlocks.NewManagementLocksClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("locks client: %w", err)
	}

	pager := client.NewListAtResourceLevelPager(
		extractRGFromID(resourceID),
		extractProviderFromID(resourceID),
		"",
		extractResourceTypeFromID(resourceID),
		extractResourceNameFromID(resourceID),
		nil,
	)

	var locks []providers.LockInfo
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing locks for %s: %w", resourceID, err)
		}
		for _, lock := range page.Value {
			if lock.Properties != nil && lock.Properties.Level != nil {
				li := providers.LockInfo{
					Level: string(*lock.Properties.Level),
					Scope: "resource",
				}
				if lock.Properties.Notes != nil {
					li.Notes = *lock.Properties.Notes
				}
				locks = append(locks, li)
			}
		}
	}
	return locks, nil
}

// ListResourceGroupLocks returns management locks on a resource group.
func (a *AzureProvider) ListResourceGroupLocks(ctx context.Context, resourceGroup string) ([]providers.LockInfo, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armlocks.NewManagementLocksClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("locks client: %w", err)
	}

	pager := client.NewListAtResourceGroupLevelPager(resourceGroup, nil)

	var locks []providers.LockInfo
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing locks for RG %s: %w", resourceGroup, err)
		}
		for _, lock := range page.Value {
			if lock.Properties != nil && lock.Properties.Level != nil {
				li := providers.LockInfo{
					Level: string(*lock.Properties.Level),
					Scope: "resourceGroup",
				}
				if lock.Properties.Notes != nil {
					li.Notes = *lock.Properties.Notes
				}
				locks = append(locks, li)
			}
		}
	}
	return locks, nil
}

// ListDiskSnapshots returns all snapshots in a resource group.
func (a *AzureProvider) ListDiskSnapshots(ctx context.Context, resourceGroup string) ([]providers.SnapshotInfo, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}

	client, err := armresources.NewClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return nil, fmt.Errorf("resources client: %w", err)
	}

	pager := client.NewListByResourceGroupPager(resourceGroup, &armresources.ClientListByResourceGroupOptions{
		Filter: to.Ptr("resourceType eq 'Microsoft.Compute/snapshots'"),
	})

	var snapshots []providers.SnapshotInfo
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing snapshots in RG %s: %w", resourceGroup, err)
		}
		for _, res := range page.Value {
			snapshots = append(snapshots, providers.SnapshotInfo{
				ID:   ptrStr(res.ID),
				Name: ptrStr(res.Name),
			})
		}
	}
	return snapshots, nil
}

// ARM ID parsing helpers

func extractRGFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractProviderFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "providers") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractResourceTypeFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "providers") && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

func extractResourceNameFromID(id string) string {
	parts := strings.Split(id, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}
