package pricing

import "context"

// HoursPerMonth is the single source of truth for "billable hours per
// month" across every pricing calculation. Azure's retail meters are
// priced per hour; Microsoft's own ACM documentation standardizes on
// 730 (365 * 24 / 12). Using 30 days, 744 hours, or similar variants
// anywhere else introduces 2-4% silent drift between the monthly cost
// the retail API reports and the number we display next to a rec.
const HoursPerMonth = 730.0

// SecondsPerMonth derives from HoursPerMonth for meters priced per
// second (Azure Container Instances vCPU / memory).
const SecondsPerMonth = HoursPerMonth * 3600

// PriceRecord holds a single price point from a cloud retail pricing API.
type PriceRecord struct {
	SKU         string  `json:"sku"`
	Service     string  `json:"service"`
	Region      string  `json:"region"`
	Monthly     float64 `json:"monthly"`
	Unit        string  `json:"unit"`
	Meter       string  `json:"meter"`
	OS          string  `json:"os"`
	ProductName string  `json:"product_name"`
}

// ReservationTerm enumerates the commitment periods Azure offers for
// Reserved Instances. Using typed constants prevents a typo like
// "1year" silently missing every cache entry keyed on "1 Year".
type ReservationTerm string

const (
	Reservation1Year  ReservationTerm = "1 Year"
	Reservation3Years ReservationTerm = "3 Years"
)

// PricingProvider is the cloud-agnostic interface for retrieving resource
// prices. Each cloud (Azure, AWS, GCP) implements this using its own
// retail pricing API and local cache.
type PricingProvider interface {
	// GetVMPrice returns the monthly pay-as-you-go cost for a VM size in a region.
	GetVMPrice(ctx context.Context, vmSize, region string) (float64, error)

	// GetDiskPrice returns the monthly cost for a managed disk by SKU name and size.
	GetDiskPrice(ctx context.Context, diskSKU string, sizeGB float64, region string) (float64, error)

	// GetIPPrice returns the monthly cost for a public IP by SKU tier.
	GetIPPrice(ctx context.Context, ipSKU, region string) (float64, error)

	// GetAppServicePrice returns the monthly cost for an App Service plan SKU.
	GetAppServicePrice(ctx context.Context, skuName string, isLinux bool, region string) (float64, error)

	// GetSQLDBPrice returns the monthly cost for a SQL Database by tier.
	GetSQLDBPrice(ctx context.Context, tier, region string) (float64, error)

	// GetLoadBalancerPrice returns the monthly fixed cost for a load balancer by SKU.
	GetLoadBalancerPrice(ctx context.Context, skuTier, region string) (float64, error)

	// GetNATGatewayPrice returns the monthly fixed cost for a NAT gateway.
	GetNATGatewayPrice(ctx context.Context, region string) (float64, error)

	// GetContainerGroupPrice returns the monthly cost for a container group
	// based on allocated vCPU cores and memory in GB.
	GetContainerGroupPrice(ctx context.Context, cpuCores, memoryGB float64, region string) (float64, error)

	// GetVMReservationPrice returns the *monthly-amortized* cost of a
	// Reserved Instance commitment for the given VM size in the
	// given region over the given term. The implementation must
	// return a clear error (not zero) when the retail API has no
	// row for the requested tuple so callers can fail closed.
	GetVMReservationPrice(ctx context.Context, vmSize, region string, term ReservationTerm) (float64, error)

	// Warmup pre-loads pricing data for a region into the local cache.
	// Should be called once before resource discovery. Subsequent calls
	// within the cache TTL are no-ops.
	Warmup(ctx context.Context, region string) error
}
