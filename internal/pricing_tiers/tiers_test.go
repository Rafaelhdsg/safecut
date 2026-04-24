package pricing_tiers

import "testing"

func TestAll_hasFourTiers(t *testing.T) {
	tiers := All()
	if len(tiers) != 4 {
		t.Fatalf("expected 4 tiers, got %d", len(tiers))
	}
	ids := map[string]bool{}
	for _, tr := range tiers {
		ids[tr.ID] = true
		if tr.Price == "" || tr.CTA == "" || tr.URL == "" {
			t.Errorf("tier %q is missing price/CTA/URL", tr.Name)
		}
	}
	for _, want := range []string{"solo", "team", "enterprise", "partner"} {
		if !ids[want] {
			t.Errorf("missing tier %q", want)
		}
	}
}

func TestPaybackDays(t *testing.T) {
	tests := []struct {
		savings float64
		want    float64
	}{
		{0, 0},
		{-10, 0},
		{29, 30}, // exactly one month of Solo covers itself in 30 days
		{870, 1}, // $870/mo savings → 1 day payback
		{8234, 30.0 * 29.0 / 8234},
	}
	for _, tc := range tests {
		got := PaybackDays(tc.savings)
		if diff := got - tc.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("PaybackDays(%.2f) = %.4f, want %.4f", tc.savings, got, tc.want)
		}
	}
}

func TestROIMultiplier(t *testing.T) {
	if got := ROIMultiplier(0); got != 0 {
		t.Errorf("zero savings: %v", got)
	}
	if got := ROIMultiplier(SoloMonthlyUSD); got != 0 {
		t.Errorf("break-even savings should yield 0 multiplier, got %v", got)
	}
	// $8,234/mo vs $29/mo ≈ 283x
	got := ROIMultiplier(8234)
	if got < 280 || got > 285 {
		t.Errorf("expected ~283x ROI for $8,234/mo, got %.2f", got)
	}
}
