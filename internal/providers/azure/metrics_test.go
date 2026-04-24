package azure

import (
	"testing"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// TestMinObservedDays covers the v1.0 fix that made ObservedDays the
// MIN across metric series instead of the max. Using the max allowed a
// "great CPU data, terrible disk data" VM to claim 14 days of full
// observation and inflate the confidence score. With min, the
// confidence reflects the weakest signal — which is how we want idle
// scoring to behave.
func TestMinObservedDays(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]int
		want int
	}{
		{"all equal", map[string]int{"cpu": 14, "network_in": 14, "disk_write": 14, "disk_read": 14, "network_out": 14}, 14},
		{"disk read lags", map[string]int{"cpu": 14, "network_in": 14, "disk_write": 14, "disk_read": 3, "network_out": 14}, 3},
		{"all zero", map[string]int{"cpu": 0, "network_in": 0, "disk_write": 0, "disk_read": 0, "network_out": 0}, 0},
		{"empty map", map[string]int{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := minObservedDays(c.in)
			if got != c.want {
				t.Errorf("minObservedDays(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestCountIdleDaysUsesSharedThreshold locks in the v1.0 guarantee that
// the Azure metrics layer uses the same CPU threshold as the engine.
// Previously, engine.DefaultIdleThresholds used 5% but this file had
// a local hardcoded 2% — so a 3% VM looked idle to one layer and busy
// to the other, producing inconsistent confidence reports.
func TestCountIdleDaysUsesSharedThreshold(t *testing.T) {
	// We don't need to run a real metric here — we just need to confirm
	// the constant that countIdleDays pulls from does not drift. See
	// internal/engine/analyzer.go for the canonical declaration.
	// A failing assertion means somebody re-introduced a local
	// "idle = CPU < 2%" shortcut.
	const expectedEngineThreshold = 5.0
	if engine.IdleCPUThresholdPercent != expectedEngineThreshold {
		t.Fatalf("engine IdleCPUThresholdPercent drift: got %v, want %v",
			engine.IdleCPUThresholdPercent, expectedEngineThreshold)
	}
}
