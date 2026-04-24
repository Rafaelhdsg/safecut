package pricing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestFetchAll_Non200Fails covers the v1.0 hardening: any non-2xx from
// the Retail API must abort with an error, not silently return zero
// items. Previously a 401 or 429 response body was handed straight to
// the JSON decoder, yielding an empty cache and making every resource
// appear "unpriced".
func TestFetchAll_Non200Fails(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"unauthorized", 401},
		{"throttled", 429},
		{"server error", 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"error":"nope"}`, tc.status)
			}))
			defer srv.Close()

			p := newTestRetailClient(t, srv.URL)
			_, err := p.fetchAll(context.Background(), "serviceName eq 'Virtual Machines'")
			if err == nil {
				t.Fatalf("expected error on HTTP %d, got nil", tc.status)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tc.status)) {
				t.Errorf("error should name the status code, got: %v", err)
			}
		})
	}
}

// TestFetchAll_IncludesRequiredQuery asserts that every outbound
// request includes api-version and currencyCode=USD.
func TestFetchAll_IncludesRequiredQuery(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Items":[], "NextPageLink":""}`)
	}))
	defer srv.Close()

	p := newTestRetailClient(t, srv.URL)
	if _, err := p.fetchAll(context.Background(), "serviceName eq 'Virtual Machines'"); err != nil {
		t.Fatalf("fetchAll: %v", err)
	}

	if gotQuery.Get("api-version") != azureRetailAPIVersion {
		t.Errorf("api-version missing or wrong: got %q, want %q",
			gotQuery.Get("api-version"), azureRetailAPIVersion)
	}
	if gotQuery.Get("currencyCode") != "USD" {
		t.Errorf("currencyCode: got %q, want USD", gotQuery.Get("currencyCode"))
	}
}

// TestGetVMReservationPrice_FailsClosedOnNoMatch covers the case where
// the API call succeeds but returns zero reservation offers for the
// requested (vmSize, region, term). The method must return an error
// so reserved_instance.go can skip the rec instead of surfacing a
// wrong zero-cost recommendation.
func TestGetVMReservationPrice_FailsClosedOnNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Items":[], "NextPageLink":""}`)
	}))
	defer srv.Close()

	p := newTestRetailClient(t, srv.URL)
	_, err := p.GetVMReservationPrice(context.Background(), "Standard_D2s_v3", "eastus", Reservation1Year)
	if err == nil {
		t.Fatal("expected error when no reservation offer matches, got nil")
	}
}

// newTestRetailClient returns an AzureRetailPricing wired to send every
// outbound HTTP call to `targetURL` instead of the live Microsoft
// endpoint. The redirect preserves path & query so fetchAll's filter
// parameter still shows up at the test server.
func newTestRetailClient(t *testing.T, targetURL string) *AzureRetailPricing {
	t.Helper()
	target, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	return &AzureRetailPricing{
		loaded:       make(map[string]map[string]PriceRecord),
		reservations: make(map[string]map[string]map[ReservationTerm]float64),
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &rewriteTransport{
				target: target,
				base:   http.DefaultTransport,
			},
		},
	}
}

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.URL.Scheme = rt.target.Scheme
	r.URL.Host = rt.target.Host
	r.Host = rt.target.Host
	return rt.base.RoundTrip(r)
}
