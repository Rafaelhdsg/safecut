//go:build online

// Package pricing integration tests that ONLY touch the Azure Retail
// Prices API — a public, unauthenticated, read-only endpoint. These
// never read or modify any Azure subscription. They are gated behind
// the `online` build tag so the normal suite remains offline-only.
//
// Run with:
//
//	go test -tags=online -run Online ./internal/pricing/...
package pricing

import (
	"context"
	"testing"
	"time"
)

// TestOnline_Warmup_realEastUS verifies the real Retail API returns a
// parseable payload with prices for eastus. If this test fails with a
// non-zero VM count, either the API version drifted, the parser
// regressed, or the response shape changed.
func TestOnline_Warmup_realEastUS(t *testing.T) {
	t.Parallel()
	p := NewAzureRetailPricing()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := p.Warmup(ctx, "eastus"); err != nil {
		t.Fatalf("Warmup(eastus) failed against live API: %v", err)
	}
	p.mu.RLock()
	records := p.loaded["eastus"]
	p.mu.RUnlock()
	if len(records) == 0 {
		t.Fatal("Warmup returned zero records — schema change or empty filter")
	}
}

// TestOnline_GetVMPrice_realEastUS asks for a size we know has
// pay-as-you-go pricing in eastus. A zero here means the parser no
// longer matches the API's productName/skuName shape.
func TestOnline_GetVMPrice_realEastUS(t *testing.T) {
	t.Parallel()
	p := NewAzureRetailPricing()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	price, err := p.GetVMPrice(ctx, "Standard_D2s_v3", "eastus")
	if err != nil {
		t.Fatalf("GetVMPrice(D2s_v3, eastus) errored: %v", err)
	}
	if price <= 0 {
		t.Fatalf("GetVMPrice returned non-positive price %.4f — parser regression likely", price)
	}
	// Sanity range: D2s_v3 monthly is ~$50–$120 USD on pay-as-you-go.
	// A value outside this window suggests the HoursPerMonth conversion
	// or currency filter silently broke.
	if price < 20 || price > 500 {
		t.Fatalf("GetVMPrice=%.2f is outside the sane D2s_v3 window [$20,$500] — check currency & HoursPerMonth", price)
	}
}

// TestOnline_GetVMReservationPrice_real1Yr exercises the separate
// reservation bucket against the real API. This is the path with the
// narrowest filter, so a Microsoft-side schema change here would
// silently zero out every RI recommendation in production — exactly
// the failure mode v1.0 needs to lock down.
func TestOnline_GetVMReservationPrice_real1Yr(t *testing.T) {
	t.Parallel()
	p := NewAzureRetailPricing()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	price, err := p.GetVMReservationPrice(ctx, "Standard_D2s_v3", "eastus", Reservation1Year)
	if err != nil {
		t.Fatalf("GetVMReservationPrice(D2s_v3, eastus, 1Y) errored: %v", err)
	}
	if price <= 0 {
		t.Fatalf("reservation 1yr price = %.4f — filter drift or schema change", price)
	}
}
