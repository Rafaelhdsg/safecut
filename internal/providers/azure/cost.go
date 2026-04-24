package azure

// extractNestedString walks a chain of map[string]interface{} keys
// and returns the final string, or "" if any step is missing or not
// the expected type. Kept in this file (rather than a grab-bag utils
// file) because every caller is a pricing-adjacent property probe.
//
// Historical note: this file used to contain a large
// estimateCost / estimate<X>Cost family that shipped hardcoded
// "typical" monthly prices per SKU. Those fallbacks were removed for
// v1.0: every monetary number now comes from the live Azure Retail
// Prices API and resources with unavailable pricing are flagged
// PriceFallback=true and excluded from TotalSaving.
func extractNestedString(m map[string]interface{}, keys ...string) string {
	current := m
	for i, key := range keys {
		v, ok := current[key]
		if !ok || v == nil {
			return ""
		}
		if i == len(keys)-1 {
			if s, ok := v.(string); ok {
				return s
			}
			return ""
		}
		next, ok := v.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}
