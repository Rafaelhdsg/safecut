package defaults

// DefaultResourceTypes is the canonical list of Azure resource types scanned
// by quick-scan, apply, and policy simulate. Product copy (README, site) should
// stay aligned with len(DefaultResourceTypes).
var DefaultResourceTypes = []string{
	"Microsoft.Compute/virtualMachines",
	"Microsoft.Compute/disks",
	"Microsoft.Network/publicIPAddresses",
	"Microsoft.Network/networkInterfaces",
	"Microsoft.Web/sites",
	"Microsoft.Sql/servers/databases",
	"Microsoft.Storage/storageAccounts",
	"Microsoft.Network/loadBalancers",
	"Microsoft.Network/natGateways",
	"Microsoft.ContainerInstance/containerGroups",
}
