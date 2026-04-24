package engine

import (
	"testing"
	"time"
)

func TestAnalyzer_AnalyzeWithPolicy_allIdleSignals(t *testing.T) {
	a := DefaultAnalyzer()
	input := MetricInput{
		CPUAvgPercent:  0,
		NetworkIn:      0,
		NetworkOut:     0,
		DiskWriteBytes: 0,
		LastActive:     time.Now().Add(-8 * 24 * time.Hour),
	}
	observed := 8 * 24 * time.Hour
	out := a.AnalyzeWithPolicy(input, observed, DefaultPolicy())
	if out.Score <= 0 {
		t.Fatalf("expected positive idle score for zero activity, got %f", out.Score)
	}
	if out.Confidence <= 0 {
		t.Fatalf("expected positive confidence, got %f", out.Confidence)
	}
}

func TestAnalyzer_AnalyzeWithPolicy_activeNetworkCollapsesScore(t *testing.T) {
	a := DefaultAnalyzer()
	input := MetricInput{
		CPUAvgPercent:  0,
		NetworkIn:      1e9,
		NetworkOut:     0,
		DiskWriteBytes: 0,
		LastActive:     time.Now().Add(-8 * 24 * time.Hour),
	}
	observed := 8 * 24 * time.Hour
	out := a.AnalyzeWithPolicy(input, observed, DefaultPolicy())
	if out.Score != 0 {
		t.Fatalf("expected composite score 0 when a signal is active, got %f", out.Score)
	}
}

// TestAnalyzer_DiskReadBytesInvalidatesIdle is the regression test for
// the v1.0 fix that added DiskReadBytes to the engine's idle signals.
// Read-heavy workloads (DB replicas, query-only VMs, OS paging) were
// previously scored as "silent disk" because only DiskWriteBytes fed
// into the composite. This test asserts a VM with heavy reads but zero
// writes is NOT flagged idle.
func TestAnalyzer_DiskReadBytesInvalidatesIdle(t *testing.T) {
	a := DefaultAnalyzer()
	input := MetricInput{
		CPUAvgPercent:  0,
		NetworkIn:      0,
		NetworkOut:     0,
		DiskReadBytes:  5e9, // 5 GB read traffic
		DiskWriteBytes: 0,
		LastActive:     time.Now().Add(-8 * 24 * time.Hour),
	}
	observed := 8 * 24 * time.Hour
	out := a.AnalyzeWithPolicy(input, observed, DefaultPolicy())
	if out.Score != 0 {
		t.Fatalf("expected read-heavy VM to NOT be flagged idle, got score=%f", out.Score)
	}
}

// TestAnalyzer_IdleCPUThresholdPercent_isExported locks the shared
// threshold constant in place so nothing can silently re-introduce a
// per-file 2%/5% divergence between engine and azure/metrics.
func TestAnalyzer_IdleCPUThresholdPercent_isExported(t *testing.T) {
	if IdleCPUThresholdPercent != 5.0 {
		t.Fatalf("IdleCPUThresholdPercent contract changed: got %v, want 5.0", IdleCPUThresholdPercent)
	}
	if DefaultIdleThresholds().CPUPercent != IdleCPUThresholdPercent {
		t.Fatalf("DefaultIdleThresholds.CPUPercent drift: got %v, want %v",
			DefaultIdleThresholds().CPUPercent, IdleCPUThresholdPercent)
	}
}

func TestAnalyzer_computeConfidence_zeroObservation(t *testing.T) {
	a := DefaultAnalyzer()
	input := MetricInput{CPUAvgPercent: 0}
	out := a.AnalyzeWithPolicy(input, 0, DefaultPolicy())
	if out.Confidence != 0 {
		t.Fatalf("confidence with zero observation window: got %f", out.Confidence)
	}
}
