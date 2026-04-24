package telemetry

import "testing"

func TestSavingsBucket(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-1, "zero"},
		{0, "zero"},
		{20, "0-50"},
		{75, "50-200"},
		{500, "200-1k"},
		{2500, "1k-5k"},
		{9000, "5k+"},
	}
	for _, c := range cases {
		if got := savingsBucket(c.in); got != c.want {
			t.Errorf("savingsBucket(%.2f) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCTAEvents_noopWhenDisabled(t *testing.T) {
	// enabled defaults to false; CTAShown/CTAClicked should not panic and
	// should be safe to call before Init().
	CTAShown("solo", "unit_test", 100)
	CTAClicked("solo", "start_trial", "unit_test")
}
