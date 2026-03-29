package engine

// Recommendation represents a single optimization recommendation
// produced by the Decision Engine after evaluating cloud resources.
type Recommendation struct {
	ResourceID   string
	ResourceType string
	Action       string
	Reason       string
	Risk         RiskLevel
	MonthlySave  float64
	Analysis     *IdleAnalysis

	// AutoExecute is false when the resource's policy blocks automated action
	// (protect mode, external dependencies, or high criticality).
	AutoExecute bool

	// PolicyNote explains why auto-execution is blocked, if applicable.
	PolicyNote string
}

type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "unknown"
	}
}

// DecisionEngine evaluates cloud resources against a set of rules
// and produces prioritized, explainable recommendations.
type DecisionEngine struct {
	recommendations []Recommendation
}

func NewDecisionEngine() *DecisionEngine {
	return &DecisionEngine{}
}

func (e *DecisionEngine) AddRecommendation(r Recommendation) {
	e.recommendations = append(e.recommendations, r)
}

func (e *DecisionEngine) Recommendations() []Recommendation {
	return e.recommendations
}
