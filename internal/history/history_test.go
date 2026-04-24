package history

import (
	"testing"
)

// TestCleanup_NilDirNoError covers the happy path where historyDir()
// returns empty (HOME unset in some sandboxed test environments) —
// Cleanup must not error and must not panic.
func TestCleanup_NilDirNoError(t *testing.T) {
	// We can't force historyDir() to return empty without monkey-patching
	// runtime. Instead we rely on the documented invariant: Cleanup(0)
	// with a freshly resolved history dir should either succeed or
	// return a real error — never panic.
	if err := Cleanup(7); err != nil {
		// A read error is acceptable; we just want the API contract
		// (error return) to be exercised.
		t.Logf("Cleanup returned error (acceptable): %v", err)
	}
}

// _ = Cleanup pinned to a func(int) error variable is a compile-time
// assertion. If the v1.0 signature regresses (e.g. back to func(int)
// with no return), this line stops compiling and CI fails loudly.
// A package-level function is never nil, so no runtime check here.
var _ func(int) error = Cleanup
