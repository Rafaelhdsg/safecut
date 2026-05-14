package engine

import "strings"

// PolicyMode defines what SafeCut is allowed to DO with a resource.
type PolicyMode string

const (
	ModeDefault PolicyMode = ""
	ModeIgnore  PolicyMode = "ignore"
	ModeObserve PolicyMode = "observe"
	ModeProtect PolicyMode = "protect"
)

// Criticality defines how IMPORTANT a resource is to the business.
type Criticality string

const (
	CriticalityNone   Criticality = ""
	CriticalityLow    Criticality = "low"
	CriticalityMedium Criticality = "medium"
	CriticalityHigh   Criticality = "high"
)

// ResourcePolicy is the base set of governance values.
// ResolvedPolicy (in policy.go) extends this with source tracking.
type ResourcePolicy struct {
	Mode         PolicyMode
	Criticality  Criticality
	ExternalDeps bool
}

func DefaultPolicy() ResourcePolicy {
	return ResourcePolicy{Mode: ModeDefault, Criticality: CriticalityNone}
}

func (p ResourcePolicy) BlocksAutoExecution() bool {
	return p.Mode == ModeProtect || p.ExternalDeps || p.Criticality == CriticalityHigh
}

func (p ResourcePolicy) RiskAdjustment() int {
	adj := 0
	if p.ExternalDeps {
		adj++
	}
	if p.Criticality == CriticalityHigh {
		adj++
	}
	return adj
}

func (p ResourcePolicy) ConfidencePenalty() float64 {
	if p.ExternalDeps {
		return 0.5
	}
	return 1.0
}

func (p ResourcePolicy) ThresholdMultiplier() float64 {
	switch p.Criticality {
	case CriticalityHigh:
		return 0.5
	case CriticalityMedium:
		return 1.0
	case CriticalityLow:
		return 2.0
	default:
		return 1.0
	}
}

// ProtectedResource records why a resource in ignore mode was excluded.
type ProtectedResource struct {
	ResourceID   string
	ResourceName string
	ResourceType string
	Policy       *ResolvedPolicy
}

// ObservedResource records an observe-mode resource with its analysis.
type ObservedResource struct {
	ResourceID   string
	ResourceName string
	ResourceType string
	Analysis     *IdleAnalysis
}

// BumpRisk increases a RiskLevel by n steps, capping at RiskHigh.
func BumpRisk(r RiskLevel, n int) RiskLevel {
	bumped := int(r) + n
	if bumped > int(RiskHigh) {
		return RiskHigh
	}
	return RiskLevel(bumped)
}

// Tag keys recognized by SafeCut (case-insensitive).
var (
	modeTags        = []string{"safecut-mode", "safecut:mode", "safecut-ignore", "safecut:ignore", "safecut-lock", "safecut:lock"}
	criticalityTags = []string{"safecut-criticality", "safecut:criticality"}
	externalTags    = []string{"safecut-external", "safecut:external"}
)

var truthyValues = map[string]bool{
	"true": true,
	"yes":  true,
	"1":    true,
}

func parseMode(tags map[string]string) PolicyMode {
	for _, key := range modeTags {
		for k, v := range tags {
			if !strings.EqualFold(k, key) {
				continue
			}
			val := strings.ToLower(strings.TrimSpace(v))
			if key == "safecut-mode" || key == "safecut:mode" {
				switch val {
				case "ignore":
					return ModeIgnore
				case "observe":
					return ModeObserve
				case "protect":
					return ModeProtect
				}
				continue
			}
			if truthyValues[val] {
				return ModeIgnore
			}
		}
	}
	return ModeDefault
}

func parseCriticality(tags map[string]string) Criticality {
	for _, key := range criticalityTags {
		for k, v := range tags {
			if !strings.EqualFold(k, key) {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "high":
				return CriticalityHigh
			case "medium":
				return CriticalityMedium
			case "low":
				return CriticalityLow
			}
		}
	}
	return CriticalityNone
}

func parseExternal(tags map[string]string) bool {
	for _, key := range externalTags {
		for k, v := range tags {
			if strings.EqualFold(k, key) && truthyValues[strings.ToLower(strings.TrimSpace(v))] {
				return true
			}
		}
	}
	return false
}
