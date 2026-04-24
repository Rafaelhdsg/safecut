package engine

import (
	"fmt"
	"math"
	"time"
)

// MetricInput is the engine's own contract for raw metric data.
// The pipeline maps provider-specific metrics into this before analysis.
// This keeps the engine independent of the discovery layer.
type MetricInput struct {
	CPUAvgPercent  float64
	NetworkIn      float64
	NetworkOut     float64
	DiskReadBytes  float64
	DiskWriteBytes float64
	LastActive     time.Time
}

// SignalWeights controls how much each signal contributes to the
// composite idle score. Different workload profiles need different
// weights — a database server cares more about disk; an API gateway
// cares more about network.
type SignalWeights struct {
	CPU        float64 `json:"cpu"`
	NetworkIn  float64 `json:"network_in"`
	NetworkOut float64 `json:"network_out"`
	DiskWrite  float64 `json:"disk_write"`
}

func DefaultSignalWeights() SignalWeights {
	return SignalWeights{
		CPU:        0.25,
		NetworkIn:  0.30,
		NetworkOut: 0.25,
		DiskWrite:  0.20,
	}
}

// IdleCPUThresholdPercent is the single source of truth for "CPU
// counts as idle below this percentage". Used by both the analyzer
// (to classify signals) and the metrics layer (to count idle days).
// Previously the two used 5% and 2% respectively, which meant the
// "idle days" figure was up to 3pp looser than the engine's own
// definition of idle — rare in practice but a classic source of
// "why does it say 12/14 idle but the analysis says active?".
const IdleCPUThresholdPercent = 5.0

// IdleThresholds controls what "idle" means for each signal.
// Values below the threshold are considered inactive.
type IdleThresholds struct {
	CPUPercent      float64       `json:"cpu_percent"`
	NetworkBytesIn  float64       `json:"network_bytes_in"`
	NetworkBytesOut float64       `json:"network_bytes_out"`
	DiskWriteBytes  float64       `json:"disk_write_bytes"`
	MinObservation  time.Duration `json:"min_observation"`
}

func DefaultIdleThresholds() IdleThresholds {
	return IdleThresholds{
		CPUPercent:      IdleCPUThresholdPercent,
		NetworkBytesIn:  1024,
		NetworkBytesOut: 1024,
		DiskWriteBytes:  1024,
		MinObservation:  7 * 24 * time.Hour,
	}
}

// SignalStatus classifies a signal's activity level.
type SignalStatus string

const (
	SignalIdle        SignalStatus = "idle"
	SignalLowActivity SignalStatus = "low_activity"
	SignalActive      SignalStatus = "active"
)

// SignalResult is the per-signal breakdown — the core of explainability.
type SignalResult struct {
	Name   string       `json:"name"`
	Value  float64      `json:"value"`
	Score  float64      `json:"score"`
	Weight float64      `json:"weight"`
	Status SignalStatus `json:"status"`
}

func (s SignalResult) String() string {
	return fmt.Sprintf("%s: %s (value=%.2f, score=%.2f)", s.Name, s.Status, s.Value, s.Score)
}

// IdleAnalysis is the complete output of analyzing a resource's idle state.
type IdleAnalysis struct {
	Score      float64        `json:"score"`
	Confidence float64        `json:"confidence"`
	Signals    []SignalResult `json:"signals"`
}

// Explain returns a human-readable summary of why this resource
// was flagged (or not) — this is the "senior engineer's reasoning".
func (a IdleAnalysis) Explain() string {
	summary := fmt.Sprintf("Idle Score: %.2f | Confidence: %.2f\n", a.Score, a.Confidence)
	for _, s := range a.Signals {
		summary += fmt.Sprintf("  - %s\n", s)
	}
	return summary
}

// Analyzer computes idle scores from raw metrics using configurable
// thresholds and weights. Swappable config enables A/B testing and
// workload-specific tuning without changing code.
type Analyzer struct {
	thresholds IdleThresholds
	weights    SignalWeights
}

func NewAnalyzer(t IdleThresholds, w SignalWeights) *Analyzer {
	return &Analyzer{thresholds: t, weights: w}
}

func DefaultAnalyzer() *Analyzer {
	return NewAnalyzer(DefaultIdleThresholds(), DefaultSignalWeights())
}

// AnalyzeWithPolicy evaluates metrics with policy-aware adjustments:
//   - Criticality shifts idle thresholds (high = stricter, low = more aggressive)
//   - External dependencies reduce confidence (incomplete visibility)
func (a *Analyzer) AnalyzeWithPolicy(input MetricInput, observedFor time.Duration, policy ResourcePolicy) IdleAnalysis {
	thresholdMult := policy.ThresholdMultiplier()
	// Combined disk I/O = reads + writes. Reads are important
	// (healthy database replicas, query-only workloads, OS paging)
	// and the old code dropped them, which caused read-heavy VMs to
	// score as "silent disk" and inflate the idle score.
	diskIO := input.DiskReadBytes + input.DiskWriteBytes
	signals := []SignalResult{
		a.evaluateSignal("cpu", input.CPUAvgPercent, a.thresholds.CPUPercent*thresholdMult, a.weights.CPU),
		a.evaluateSignal("network_in", input.NetworkIn, a.thresholds.NetworkBytesIn*thresholdMult, a.weights.NetworkIn),
		a.evaluateSignal("network_out", input.NetworkOut, a.thresholds.NetworkBytesOut*thresholdMult, a.weights.NetworkOut),
		a.evaluateSignal("disk_io", diskIO, a.thresholds.DiskWriteBytes*thresholdMult, a.weights.DiskWrite),
	}

	scores := make([]float64, len(signals))
	weights := make([]float64, len(signals))
	for i, s := range signals {
		scores[i] = s.Score
		weights[i] = s.Weight
	}

	confidence := a.computeConfidence(input, observedFor) * policy.ConfidencePenalty()

	return IdleAnalysis{
		Score:      weightedGeometricMean(scores, weights),
		Confidence: confidence,
		Signals:    signals,
	}
}

func (a *Analyzer) evaluateSignal(name string, value, threshold, weight float64) SignalResult {
	score := clampSignal(value, threshold)
	return SignalResult{
		Name:   name,
		Value:  value,
		Score:  score,
		Weight: weight,
		Status: classifySignal(score),
	}
}

func classifySignal(score float64) SignalStatus {
	switch {
	case score >= 0.8:
		return SignalIdle
	case score >= 0.4:
		return SignalLowActivity
	default:
		return SignalActive
	}
}

// clampSignal converts a raw metric into a 0–1 "idle-ness" score.
// Returns 1.0 when value is 0 (fully idle), approaches 0.0 as value
// reaches or exceeds the threshold.
func clampSignal(value, threshold float64) float64 {
	if threshold <= 0 {
		return 0
	}
	ratio := value / threshold
	if ratio >= 1.0 {
		return 0
	}
	return 1.0 - ratio
}

// weightedGeometricMean computes ∏(score_i ^ weight_i).
// Any single active signal (score ≈ 0) collapses the composite,
// preventing false positives from single-dimension checks.
func weightedGeometricMean(scores, weights []float64) float64 {
	if len(scores) == 0 || len(scores) != len(weights) {
		return 0
	}
	logSum := 0.0
	for i, s := range scores {
		if s <= 0 {
			return 0
		}
		logSum += weights[i] * math.Log(s)
	}
	return math.Exp(logSum)
}

func (a *Analyzer) computeConfidence(input MetricInput, observed time.Duration) float64 {
	if observed <= 0 {
		return 0
	}

	// Confidence scales linearly with observation window length.
	// 7+ days of data → maximum confidence.
	// Zero metric values from a real monitoring source are genuine
	// readings (the resource IS idle), not missing data.
	return math.Min(float64(observed)/float64(a.thresholds.MinObservation), 1.0)
}
