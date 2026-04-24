package pricing

import "testing"

// TestTimeConstants locks the v1.0 single-source-of-truth values.
// The CLI multiplies per-hour and per-second Retail API prices by
// these constants; drifting from 730 would silently shift every
// monthly price by ~2-3% (744-hour months) and break reconciliation
// with Azure Cost Management, which also uses 730.
func TestTimeConstants(t *testing.T) {
	if HoursPerMonth != 730.0 {
		t.Errorf("HoursPerMonth: got %v, want 730.0", HoursPerMonth)
	}
	if SecondsPerMonth != 730.0*3600 {
		t.Errorf("SecondsPerMonth: got %v, want %v", SecondsPerMonth, 730.0*3600)
	}
}

// TestReservationTermValues pins the API strings used in the $filter
// parameter. A silent change here would produce zero Retail API rows
// for every RI query and make the reserved_instance rule disappear
// from the output entirely.
func TestReservationTermValues(t *testing.T) {
	if string(Reservation1Year) != "1 Year" {
		t.Errorf("Reservation1Year: got %q, want \"1 Year\"", Reservation1Year)
	}
	if string(Reservation3Years) != "3 Years" {
		t.Errorf("Reservation3Years: got %q, want \"3 Years\"", Reservation3Years)
	}
}
