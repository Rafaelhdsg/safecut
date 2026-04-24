package azure

import (
	"strings"
	"testing"
)

// TestParseMap_HappyPath covers a realistic nested map. The function
// must round-trip through json and return the original keys intact.
func TestParseMap_HappyPath(t *testing.T) {
	in := map[string]interface{}{
		"provisioningState": "Succeeded",
		"diskState":         "Unattached",
		"nested": map[string]interface{}{
			"inner": "value",
		},
	}
	out, err := parseMap(in)
	if err != nil {
		t.Fatalf("parseMap: %v", err)
	}
	if out["provisioningState"] != "Succeeded" {
		t.Errorf("lost provisioningState, got %v", out["provisioningState"])
	}
	if out["diskState"] != "Unattached" {
		t.Errorf("lost diskState, got %v", out["diskState"])
	}
}

// TestParseMap_NilReturnsNil preserves the "no properties" signal.
// Callers rely on nil returning (nil, nil) so they can skip rows that
// truly lack a properties block instead of treating them as errors.
func TestParseMap_NilReturnsNil(t *testing.T) {
	out, err := parseMap(nil)
	if err != nil {
		t.Fatalf("parseMap(nil) err = %v, want nil", err)
	}
	if out != nil {
		t.Errorf("parseMap(nil) map = %v, want nil", out)
	}
}

// TestParseMap_NonMapPropagatesError is the regression test for the v1.0
// fix that stopped silently swallowing unexpected types. Resource Graph
// has been observed to return properties as a string in edge cases
// (e.g. Microsoft.Web/sites with transient hydration); the old code
// returned nil and treated them as "no properties", which is wrong.
// Now we surface a Marshal/Unmarshal error so the caller can log and
// skip the row.
func TestParseMap_NonMapPropagatesError(t *testing.T) {
	// A raw string can't unmarshal into a map[string]interface{}.
	_, err := parseMap("not-a-map")
	if err == nil {
		t.Fatal("expected error when value is not marshalable to map, got nil")
	}
	// The error should name the unmarshal step so operators can grep
	// logs for "parseMap" or "unmarshal map".
	if !strings.Contains(strings.ToLower(err.Error()), "unmarshal") &&
		!strings.Contains(strings.ToLower(err.Error()), "parsemap") {
		t.Errorf("error should mention the unmarshal step, got: %v", err)
	}
}

// TestExtractNestedString_Missing asserts the utility returns an empty
// string (not panic) when any step in the chain is missing or has the
// wrong type. This contract matters because pricing code uses it on
// every resource and a panic would take the whole scan down.
func TestExtractNestedString_Missing(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "found",
		},
		"wrongType": 42,
	}

	if got := extractNestedString(m, "a", "b"); got != "found" {
		t.Errorf("hit: got %q, want found", got)
	}
	if got := extractNestedString(m, "a", "missing"); got != "" {
		t.Errorf("missing leaf: got %q, want empty", got)
	}
	if got := extractNestedString(m, "wrongType", "x"); got != "" {
		t.Errorf("non-map: got %q, want empty", got)
	}
	if got := extractNestedString(nil, "a"); got != "" {
		t.Errorf("nil map: got %q, want empty", got)
	}
}
