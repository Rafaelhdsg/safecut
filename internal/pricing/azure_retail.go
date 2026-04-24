package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	azureRetailAPI = "https://prices.azure.com/api/retail/prices"
	// azureRetailAPIVersion pins the API version so a Microsoft-side
	// default change can't silently alter the response shape and
	// introduce "phantom zero pricing".
	azureRetailAPIVersion = "2023-01-01-preview"
	// azureRetailCurrency is explicit so the default-currency
	// negotiation Microsoft does based on caller IP can't surface
	// non-USD numbers next to our "$X.XX" copy.
	azureRetailCurrency = "USD"
	fetchTimeout        = 30 * time.Second
	cloudAzure          = "azure"
)

// AzureRetailPricing implements PricingProvider using the public
// Azure Retail Prices API. No authentication required.
type AzureRetailPricing struct {
	mu sync.RWMutex
	// loaded is the consumption (pay-as-you-go) cache:
	//   region -> key -> record
	loaded map[string]map[string]PriceRecord
	// reservations is a separate cache keyed by (region, vmSize, term)
	// because the reservation filter is narrower and only worth
	// fetching on-demand per rec. Populated lazily.
	//   region -> vmSize -> term -> monthly amortized price
	reservations map[string]map[string]map[ReservationTerm]float64
	client       *http.Client
}

func NewAzureRetailPricing() *AzureRetailPricing {
	return &AzureRetailPricing{
		loaded:       make(map[string]map[string]PriceRecord),
		reservations: make(map[string]map[string]map[ReservationTerm]float64),
		client:       &http.Client{Timeout: fetchTimeout},
	}
}

// Warmup fetches pricing for a region from cache or API.
// Subsequent calls for the same region are no-ops.
func (p *AzureRetailPricing) Warmup(ctx context.Context, region string) error {
	region = strings.ToLower(region)
	p.mu.RLock()
	_, ok := p.loaded[region]
	p.mu.RUnlock()
	if ok {
		return nil
	}

	if cacheIsValid(cloudAzure, region) {
		records, err := cacheLoad(cloudAzure, region)
		if err == nil && len(records) > 0 {
			p.mu.Lock()
			p.loaded[region] = records
			p.mu.Unlock()
			return nil
		}
	}

	records, err := p.fetchRegion(ctx, region)
	if err != nil {
		return err
	}
	_ = cacheSave(cloudAzure, region, records)

	p.mu.Lock()
	p.loaded[region] = records
	p.mu.Unlock()
	return nil
}

func (p *AzureRetailPricing) GetVMPrice(ctx context.Context, vmSize, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	key := vmKey(vmSize)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for VM %s in %s", vmSize, region)
}

func (p *AzureRetailPricing) GetDiskPrice(ctx context.Context, diskSKU string, sizeGB float64, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	// Try exact managed disk SKU match (e.g., "Premium_LRS" -> P10, P20, etc.)
	diskTier := mapDiskSKUToTier(diskSKU, sizeGB)
	key := diskKey(diskTier)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}

	// Fallback: per-GB estimate from cached disk pricing
	key = diskKey(strings.ToLower(diskSKU))
	if rec, ok := records[key]; ok && rec.Monthly > 0 {
		return rec.Monthly, nil
	}

	return 0, fmt.Errorf("no pricing found for disk %s (%dGB) in %s", diskSKU, int(sizeGB), region)
}

func (p *AzureRetailPricing) GetIPPrice(ctx context.Context, ipSKU, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	key := ipKey(ipSKU)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	// Default standard IP key
	if rec, ok := records[ipKey("standard")]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for IP %s in %s", ipSKU, region)
}

func (p *AzureRetailPricing) GetAppServicePrice(ctx context.Context, skuName string, isLinux bool, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	plan := strings.ToLower(skuName)
	osTag := "linux"
	if !isLinux {
		osTag = "windows"
	}

	key := appKey(plan + ":" + osTag)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	// Fallback: try the other OS
	altOS := "windows"
	if osTag == "windows" {
		altOS = "linux"
	}
	key = appKey(plan + ":" + altOS)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for App Service %s in %s", skuName, region)
}

func (p *AzureRetailPricing) GetSQLDBPrice(ctx context.Context, tier, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	key := sqldbKey(tier)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for SQL DB tier %s in %s", tier, region)
}

func (p *AzureRetailPricing) GetLoadBalancerPrice(ctx context.Context, skuTier, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	key := lbKey(skuTier)
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	// Default to standard
	if rec, ok := records[lbKey("standard")]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for Load Balancer %s in %s", skuTier, region)
}

func (p *AzureRetailPricing) GetNATGatewayPrice(ctx context.Context, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	key := natgwKey()
	if rec, ok := records[key]; ok {
		return rec.Monthly, nil
	}
	return 0, fmt.Errorf("no pricing found for NAT Gateway in %s", region)
}

func (p *AzureRetailPricing) GetContainerGroupPrice(ctx context.Context, cpuCores, memoryGB float64, region string) (float64, error) {
	region = strings.ToLower(region)
	if err := p.Warmup(ctx, region); err != nil {
		return 0, err
	}

	p.mu.RLock()
	records := p.loaded[region]
	p.mu.RUnlock()

	var total float64
	if rec, ok := records[ciKey("vcpu")]; ok {
		total += rec.Monthly * cpuCores
	}
	if rec, ok := records[ciKey("memory")]; ok {
		total += rec.Monthly * memoryGB
	}
	if total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("no pricing found for Container Instances in %s", region)
}

// fetchRegion pulls pricing for all supported resource types in a region from the API.
func (p *AzureRetailPricing) fetchRegion(ctx context.Context, region string) (map[string]PriceRecord, error) {
	records := make(map[string]PriceRecord)

	services := []struct {
		name    string
		filter  string
		process func(item apiItem, records map[string]PriceRecord)
	}{
		{
			name:   "Virtual Machines",
			filter: fmt.Sprintf("serviceName eq 'Virtual Machines' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				if item.ArmSkuName == "" {
					return
				}
				// Skip Windows, Spot, and Low Priority — we want Linux PAYG
				lower := strings.ToLower(item.SkuName)
				if strings.Contains(lower, "windows") || strings.Contains(lower, "spot") || strings.Contains(lower, "low priority") {
					return
				}
				meterLower := strings.ToLower(item.MeterName)
				if strings.Contains(meterLower, "spot") || strings.Contains(meterLower, "low priority") {
					return
				}
				prodLower := strings.ToLower(item.ProductName)
				if strings.Contains(prodLower, "windows") {
					return
				}

				monthly := item.RetailPrice * HoursPerMonth
				key := vmKey(item.ArmSkuName)
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					// Keep the lower price (some SKUs have multiple meters)
					if monthly >= existing.Monthly {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         item.ArmSkuName,
					Service:     "Virtual Machines",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					OS:          "Linux",
					ProductName: item.ProductName,
				}
			},
		},
		{
			name:   "Storage",
			filter: fmt.Sprintf("serviceName eq 'Storage' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				skuLower := strings.ToLower(item.SkuName)
				// We want managed disk LRS pricing (P10 LRS, E30 LRS, S10 LRS, etc.)
				if !strings.Contains(skuLower, "lrs") && !strings.Contains(skuLower, "zrs") {
					return
				}
				if !isDiskMeter(meterLower) {
					return
				}
				// Prefer LRS over ZRS
				diskID := extractDiskTierFromMeter(meterLower)
				if diskID == "" {
					return
				}
				key := diskKey(diskID)
				monthly := item.RetailPrice
				// Some disk meters are per-month already, some are per-unit
				if strings.Contains(strings.ToLower(item.UnitOfMeasure), "hour") {
					monthly = item.RetailPrice * HoursPerMonth
				}
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					if strings.Contains(skuLower, "zrs") {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         item.SkuName,
					Service:     "Storage",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					ProductName: item.ProductName,
				}
			},
		},
		{
			name:   "Networking",
			filter: fmt.Sprintf("serviceName eq 'Virtual Network' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				if !strings.Contains(meterLower, "public ip") && !strings.Contains(meterLower, "ip address") {
					return
				}
				skuLower := strings.ToLower(item.SkuName)
				tier := "standard"
				if strings.Contains(skuLower, "basic") {
					tier = "basic"
				}
				monthly := item.RetailPrice * HoursPerMonth
				if strings.Contains(strings.ToLower(item.UnitOfMeasure), "month") {
					monthly = item.RetailPrice
				}
				key := ipKey(tier)
				if _, ok := records[key]; !ok || monthly > 0 {
					records[key] = PriceRecord{
						SKU:         tier,
						Service:     "Networking",
						Region:      region,
						Monthly:     monthly,
						Unit:        item.UnitOfMeasure,
						Meter:       item.MeterName,
						ProductName: item.ProductName,
					}
				}
			},
		},
		{
			name:   "App Service",
			filter: fmt.Sprintf("serviceName eq 'Azure App Service' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				skuLower := strings.ToLower(item.SkuName)
				meterLower := strings.ToLower(item.MeterName)
				prodLower := strings.ToLower(item.ProductName)
				if strings.Contains(skuLower, "spot") || strings.Contains(meterLower, "stamp") {
					return
				}
				// Map known plan names (B1, B2, B3, S1, S2, S3, P1v2, P1v3, etc.)
				planName := extractAppServicePlan(skuLower, meterLower)
				if planName == "" {
					return
				}
				monthly := item.RetailPrice * HoursPerMonth
				if strings.Contains(strings.ToLower(item.UnitOfMeasure), "month") {
					monthly = item.RetailPrice
				}
				// Store separate keys for linux vs windows
				osTag := "linux"
				if strings.Contains(prodLower, "windows") {
					osTag = "windows"
				}
				key := appKey(planName + ":" + osTag)
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					if monthly >= existing.Monthly {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         planName,
					Service:     "App Service",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					OS:          osTag,
					ProductName: item.ProductName,
				}
			},
		},
		{
			name:   "SQL Database",
			filter: fmt.Sprintf("serviceName eq 'SQL Database' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				prodLower := strings.ToLower(item.ProductName)
				// We want DTU-based tiers (Basic, Standard S0-S12, Premium P1-P15)
				// and vCore General Purpose / Business Critical
				tier := extractSQLDBTier(meterLower, prodLower)
				if tier == "" {
					return
				}
				monthly := item.RetailPrice * HoursPerMonth
				unitLower := strings.ToLower(item.UnitOfMeasure)
				if strings.Contains(unitLower, "month") || strings.Contains(unitLower, "/month") {
					monthly = item.RetailPrice
				} else if strings.Contains(unitLower, "day") || strings.Contains(unitLower, "/day") {
					// A 30-day month silently underreports monthly
					// cost by ~1.6% vs the 730-hour Azure convention.
					// Match HoursPerMonth / 24 to keep every meter on
					// the same yardstick.
					monthly = item.RetailPrice * (HoursPerMonth / 24)
				}
				key := sqldbKey(tier)
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					if monthly >= existing.Monthly {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         tier,
					Service:     "SQL Database",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					ProductName: item.ProductName,
				}
			},
		},
		{
			name:   "Load Balancer",
			filter: fmt.Sprintf("serviceName eq 'Load Balancer' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				if !strings.Contains(meterLower, "rule") && !strings.Contains(meterLower, "load balancer") {
					return
				}
				skuLower := strings.ToLower(item.SkuName)
				tier := "standard"
				if strings.Contains(skuLower, "basic") {
					tier = "basic"
				}
				monthly := item.RetailPrice * HoursPerMonth
				if strings.Contains(strings.ToLower(item.UnitOfMeasure), "month") {
					monthly = item.RetailPrice
				}
				key := lbKey(tier)
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					if monthly >= existing.Monthly {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         tier,
					Service:     "Load Balancer",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					ProductName: item.ProductName,
				}
			},
		},
		{
			name:   "NAT Gateway",
			filter: fmt.Sprintf("serviceName eq 'NAT Gateway' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				if !strings.Contains(meterLower, "nat gateway") && !strings.Contains(meterLower, "gateway") {
					return
				}
				// Skip data processing meters, keep base resource hour
				if strings.Contains(meterLower, "data processed") {
					return
				}
				monthly := item.RetailPrice * HoursPerMonth
				if strings.Contains(strings.ToLower(item.UnitOfMeasure), "month") {
					monthly = item.RetailPrice
				}
				key := natgwKey()
				if _, ok := records[key]; !ok || monthly > 0 {
					records[key] = PriceRecord{
						SKU:         "standard",
						Service:     "NAT Gateway",
						Region:      region,
						Monthly:     monthly,
						Unit:        item.UnitOfMeasure,
						Meter:       item.MeterName,
						ProductName: item.ProductName,
					}
				}
			},
		},
		{
			name:   "Container Instances",
			filter: fmt.Sprintf("serviceName eq 'Container Instances' and armRegionName eq '%s' and priceType eq 'Consumption'", region),
			process: func(item apiItem, records map[string]PriceRecord) {
				meterLower := strings.ToLower(item.MeterName)
				prodLower := strings.ToLower(item.ProductName)
				// Skip GPU and Windows dedicated meters
				if strings.Contains(prodLower, "gpu") || strings.Contains(prodLower, "dedicated") {
					return
				}
				// Per-second pricing: vCPU Duration, Memory Duration
				var key string
				if strings.Contains(meterLower, "vcpu") || strings.Contains(meterLower, "cpu") {
					key = ciKey("vcpu")
				} else if strings.Contains(meterLower, "memory") || strings.Contains(meterLower, "mem") {
					key = ciKey("memory")
				} else {
					return
				}
				// Convert per-second price to monthly. API returns
				// $/second; see pricing.SecondsPerMonth for the
				// derivation (2,628,000 seconds/month).
				monthly := item.RetailPrice * SecondsPerMonth
				unitLower := strings.ToLower(item.UnitOfMeasure)
				if strings.Contains(unitLower, "hour") {
					monthly = item.RetailPrice * HoursPerMonth
				}
				if existing, ok := records[key]; ok && existing.Monthly > 0 {
					if monthly >= existing.Monthly {
						return
					}
				}
				records[key] = PriceRecord{
					SKU:         item.SkuName,
					Service:     "Container Instances",
					Region:      region,
					Monthly:     monthly,
					Unit:        item.UnitOfMeasure,
					Meter:       item.MeterName,
					ProductName: item.ProductName,
				}
			},
		},
	}

	for _, svc := range services {
		items, err := p.fetchAll(ctx, svc.filter)
		if err != nil {
			return records, fmt.Errorf("fetch %s prices: %w", svc.name, err)
		}
		for _, item := range items {
			svc.process(item, records)
		}
	}

	return records, nil
}

// fetchAll paginates through the Azure Retail Prices API.
//
// Every request is pinned to a known api-version and currencyCode, and
// any non-200 status aborts with a descriptive error. Prior versions
// of this function decoded the body regardless of status, which meant
// a 401/403/5xx (whose body is an error envelope, not a price list)
// silently became an empty result — and the whole CLI fell back to
// hardcoded "typical" prices.
func (p *AzureRetailPricing) fetchAll(ctx context.Context, filter string) ([]apiItem, error) {
	var all []apiItem

	// Build the initial URL with explicit api-version and currency.
	// NextPageLink responses already include these params, so the
	// pagination loop doesn't need to re-append them.
	q := url.Values{}
	q.Set("api-version", azureRetailAPIVersion)
	q.Set("currencyCode", azureRetailCurrency)
	q.Set("$filter", filter)
	nextURL := azureRetailAPI + "?" + q.Encode()

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return all, err
		}

		resp, err := p.client.Do(req)
		if err != nil {
			return all, err
		}

		if resp.StatusCode != http.StatusOK {
			// Read a bounded amount of the error body for the message
			// but don't stream it into the JSON decoder — the shape is
			// an error envelope, not an apiResponse.
			var buf [512]byte
			n, _ := resp.Body.Read(buf[:])
			resp.Body.Close()
			return all, fmt.Errorf(
				"azure retail prices returned HTTP %d: %s",
				resp.StatusCode, strings.TrimSpace(string(buf[:n])),
			)
		}

		var body apiResponse
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return all, fmt.Errorf("decode pricing response: %w", err)
		}

		all = append(all, body.Items...)
		nextURL = body.NextPageLink
	}
	return all, nil
}

// apiResponse is the top-level response from Azure Retail Prices API.
type apiResponse struct {
	Items        []apiItem `json:"Items"`
	NextPageLink string    `json:"NextPageLink"`
	Count        int       `json:"Count"`
}

// apiItem is a single price item from the API.
type apiItem struct {
	CurrencyCode    string  `json:"currencyCode"`
	RetailPrice     float64 `json:"retailPrice"`
	UnitPrice       float64 `json:"unitPrice"`
	ArmRegionName   string  `json:"armRegionName"`
	Location        string  `json:"location"`
	MeterName       string  `json:"meterName"`
	SkuName         string  `json:"skuName"`
	ProductName     string  `json:"productName"`
	ServiceName     string  `json:"serviceName"`
	ServiceFamily   string  `json:"serviceFamily"`
	UnitOfMeasure   string  `json:"unitOfMeasure"`
	Type            string  `json:"type"`
	IsPrimary       bool    `json:"isPrimaryMeterRegion"`
	ArmSkuName      string  `json:"armSkuName"`
	ReservationTerm string  `json:"reservationTerm"`
}

// GetVMReservationPrice fetches the *monthly-amortized* RI price for
// the given VM size / region / term, calling the Retail Prices API on
// first use and caching per (region, size, term). A real error — not
// a silent zero — is returned if no row matches, so the caller can
// fail closed and drop the recommendation.
func (p *AzureRetailPricing) GetVMReservationPrice(
	ctx context.Context, vmSize, region string, term ReservationTerm,
) (float64, error) {
	region = strings.ToLower(region)
	vmLower := strings.ToLower(vmSize)

	p.mu.RLock()
	if regionCache, ok := p.reservations[region]; ok {
		if sizeCache, ok := regionCache[vmLower]; ok {
			if price, ok := sizeCache[term]; ok {
				p.mu.RUnlock()
				if price <= 0 {
					return 0, fmt.Errorf("no reservation price for %s in %s (%s) — cached miss", vmSize, region, term)
				}
				return price, nil
			}
		}
	}
	p.mu.RUnlock()

	termMonths := 12
	if term == Reservation3Years {
		termMonths = 36
	}

	// Reservation filter: price type is "Reservation", service is
	// "Virtual Machines", specific armSkuName + region + term. Using
	// armSkuName is important — skuName varies (e.g. "D2s v3 Low
	// Priority") and would match Spot, not standard.
	filter := fmt.Sprintf(
		"serviceName eq 'Virtual Machines' and armRegionName eq '%s' and armSkuName eq '%s' and priceType eq 'Reservation' and reservationTerm eq '%s'",
		region, vmSize, string(term),
	)
	items, err := p.fetchAll(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("reservation price fetch: %w", err)
	}
	if len(items) == 0 {
		p.cacheReservation(region, vmLower, term, 0)
		return 0, fmt.Errorf("no reservation price for %s in %s (%s)", vmSize, region, term)
	}

	// The API returns the *total* upfront price for the term under
	// unitOfMeasure "1/Hour" × hours. In practice every row carries
	// retailPrice = upfront total; amortize across term months.
	// Prefer Linux (non-Windows) SKU; if multiple, pick the lowest.
	best := -1.0
	for _, it := range items {
		if it.RetailPrice <= 0 {
			continue
		}
		lowerProd := strings.ToLower(it.ProductName)
		if strings.Contains(lowerProd, "windows") {
			continue
		}
		monthly := it.RetailPrice / float64(termMonths)
		if best < 0 || monthly < best {
			best = monthly
		}
	}
	if best <= 0 {
		// Fall back to first positive row if no Linux match.
		for _, it := range items {
			if it.RetailPrice > 0 {
				best = it.RetailPrice / float64(termMonths)
				break
			}
		}
	}
	if best <= 0 {
		p.cacheReservation(region, vmLower, term, 0)
		return 0, fmt.Errorf("no usable reservation price for %s in %s (%s)", vmSize, region, term)
	}
	p.cacheReservation(region, vmLower, term, best)
	return best, nil
}

func (p *AzureRetailPricing) cacheReservation(region, vmLower string, term ReservationTerm, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reservations == nil {
		p.reservations = make(map[string]map[string]map[ReservationTerm]float64)
	}
	if _, ok := p.reservations[region]; !ok {
		p.reservations[region] = make(map[string]map[ReservationTerm]float64)
	}
	if _, ok := p.reservations[region][vmLower]; !ok {
		p.reservations[region][vmLower] = make(map[ReservationTerm]float64)
	}
	p.reservations[region][vmLower][term] = price
}

// ── Key helpers ──

func vmKey(vmSize string) string {
	return "vm:" + strings.ToLower(vmSize)
}

func diskKey(tier string) string {
	return "disk:" + strings.ToLower(tier)
}

func ipKey(sku string) string {
	return "ip:" + strings.ToLower(sku)
}

func appKey(skuName string) string {
	return "app:" + strings.ToLower(skuName)
}

func sqldbKey(tier string) string {
	return "sqldb:" + strings.ToLower(tier)
}

func lbKey(sku string) string {
	return "lb:" + strings.ToLower(sku)
}

func natgwKey() string {
	return "natgw:standard"
}

func ciKey(meter string) string {
	return "ci:" + strings.ToLower(meter)
}

// ── Disk helpers ──

// isDiskMeter returns true if the meter name looks like a managed disk meter.
func isDiskMeter(meter string) bool {
	for _, prefix := range []string{"p", "e", "s"} {
		for _, size := range []string{"1", "2", "3", "4", "6", "10", "15", "20", "30", "40", "50", "60", "70", "80"} {
			if strings.HasPrefix(meter, prefix+size+" ") || strings.Contains(meter, prefix+size+" disk") || strings.Contains(meter, prefix+size+" lrs") {
				return true
			}
		}
	}
	return false
}

// extractDiskTierFromMeter extracts the disk tier (e.g., "p10", "e30") from a meter name.
func extractDiskTierFromMeter(meter string) string {
	parts := strings.Fields(meter)
	if len(parts) == 0 {
		return ""
	}
	candidate := strings.ToLower(parts[0])
	if len(candidate) >= 2 {
		prefix := candidate[0]
		if prefix == 'p' || prefix == 'e' || prefix == 's' {
			rest := candidate[1:]
			for _, c := range rest {
				if c < '0' || c > '9' {
					return ""
				}
			}
			return candidate
		}
	}
	return ""
}

// mapDiskSKUToTier maps an Azure managed disk SKU name and size to the
// closest pricing tier (e.g., Premium_LRS + 128GB -> "p10").
func mapDiskSKUToTier(skuName string, sizeGB float64) string {
	lower := strings.ToLower(skuName)

	var prefix string
	switch {
	case strings.Contains(lower, "premium"):
		prefix = "p"
	case strings.Contains(lower, "standardssd"), strings.Contains(lower, "standard_ssd"):
		prefix = "e"
	default:
		prefix = "s"
	}

	// Map disk size to tier number
	type tierRange struct {
		maxGB int
		tier  int
	}
	tiers := []tierRange{
		{4, 1}, {8, 2}, {16, 3}, {32, 4}, {64, 6},
		{128, 10}, {256, 15}, {512, 20}, {1024, 30},
		{2048, 40}, {4096, 50}, {8192, 60}, {16384, 70}, {32767, 80},
	}

	gb := int(math.Ceil(sizeGB))
	for _, t := range tiers {
		if gb <= t.maxGB {
			return fmt.Sprintf("%s%d", prefix, t.tier)
		}
	}
	return fmt.Sprintf("%s80", prefix)
}

// ── App Service helpers ──

// extractAppServicePlan extracts the plan name (b1, s1, p1v3, etc.) from
// an API item's SKU or meter name.
func extractAppServicePlan(skuLower, meterLower string) string {
	knownPlans := []string{
		"f1", "d1",
		"b1", "b2", "b3",
		"s1", "s2", "s3",
		"p1v2", "p2v2", "p3v2",
		"p1v3", "p2v3", "p3v3",
		"p0v3", "p1mv3", "p2mv3", "p3mv3", "p4mv3", "p5mv3",
		"i1", "i2", "i3",
		"i1v2", "i2v2", "i3v2",
		"y1",
	}
	combined := skuLower + " " + meterLower
	for _, plan := range knownPlans {
		if strings.Contains(combined, plan+" ") || strings.HasSuffix(combined, plan) || strings.Contains(combined, plan+"/") {
			return plan
		}
	}
	// Try splitting sku on spaces: "B1" in "B1 App"
	parts := strings.Fields(skuLower)
	if len(parts) > 0 {
		candidate := strings.ToLower(parts[0])
		for _, plan := range knownPlans {
			if candidate == plan {
				return plan
			}
		}
	}
	return ""
}

// ── SQL Database helpers ──

// extractSQLDBTier maps API meter/product names to a normalized tier key.
func extractSQLDBTier(meterLower, prodLower string) string {
	combined := meterLower + " " + prodLower
	switch {
	case strings.Contains(combined, "basic"):
		return "basic"
	case strings.Contains(combined, "general purpose"):
		return "generalpurpose"
	case strings.Contains(combined, "business critical"):
		return "businesscritical"
	case strings.Contains(combined, "hyperscale"):
		return "hyperscale"
	case strings.Contains(combined, "premium"):
		return "premium"
	case strings.Contains(combined, "standard"):
		return "standard"
	}
	return ""
}
