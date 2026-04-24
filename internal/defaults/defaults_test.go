package defaults

import "testing"

func TestDefaultResourceTypes_count(t *testing.T) {
	const want = 10
	if got := len(DefaultResourceTypes); got != want {
		t.Fatalf("len(DefaultResourceTypes) = %d; product copy assumes %d Azure resource types", got, want)
	}
}
