package pricing

import "testing"

func TestKeyHelpers(t *testing.T) {
	if got := vmKey("Standard_D2s_v3"); got != "vm:standard_d2s_v3" {
		t.Errorf("vmKey = %q", got)
	}
	if got := diskKey("P10"); got != "disk:p10" {
		t.Errorf("diskKey = %q", got)
	}
	if got := ipKey("Basic"); got != "ip:basic" {
		t.Errorf("ipKey = %q", got)
	}
	if got := natgwKey(); got != "natgw:standard" {
		t.Errorf("natgwKey = %q", got)
	}
}

func TestIsDiskMeter(t *testing.T) {
	if !isDiskMeter("p10 lrs disk") {
		t.Fatal("expected p10 meter to match")
	}
	if isDiskMeter("compute hours") {
		t.Fatal("unexpected match")
	}
}

func TestExtractDiskTierFromMeter(t *testing.T) {
	if got := extractDiskTierFromMeter("p30 lrs disk"); got != "p30" {
		t.Errorf("got %q", got)
	}
	if got := extractDiskTierFromMeter(""); got != "" {
		t.Errorf("empty meter: got %q", got)
	}
}
