package cmd

import (
	"strings"
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/pricing_tiers"
)

func TestResolveUpgradeAction_precedence(t *testing.T) {
	cases := []struct {
		name       string
		startTrial string
		bookDemo   bool
		partner    bool
		wantURL    string
		wantOK     bool
	}{
		{"no flags", "", false, false, "", false},
		{"solo trial", "solo", false, false, pricing_tiers.CheckoutSoloURL, true},
		{"team trial", "TEAM", false, false, pricing_tiers.CheckoutTeamURL, true},
		{"unknown trial falls back to pricing", "gold", false, false, pricing_tiers.PricingURL, true},
		{"book demo beats trial", "solo", true, false, pricing_tiers.EnterpriseURL, true},
		{"partner beats demo", "solo", true, true, pricing_tiers.PartnerURL, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label, url, ok := resolveUpgradeAction(tc.startTrial, tc.bookDemo, tc.partner)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (label=%q url=%q)", ok, tc.wantOK, label, url)
			}
			if !tc.wantOK {
				return
			}
			if url != tc.wantURL {
				t.Fatalf("url = %q, want %q", url, tc.wantURL)
			}
			if strings.TrimSpace(label) == "" {
				t.Fatalf("label must not be empty")
			}
		})
	}
}
