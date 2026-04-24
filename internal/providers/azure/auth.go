package azure

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// newCredential returns an azcore.TokenCredential using DefaultAzureCredential,
// which tries in order: environment vars, workload identity, managed identity,
// Azure CLI, Azure Developer CLI. Works locally and in CI/cloud.
func newCredential() (azcore.TokenCredential, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure auth: %w (hint: run 'az login' or set AZURE_CLIENT_ID/AZURE_TENANT_ID/AZURE_CLIENT_SECRET)", err)
	}
	return cred, nil
}
