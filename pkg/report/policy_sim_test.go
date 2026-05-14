package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/pipeline"
)

func TestRenderPolicySim_JSON_roundtrip(t *testing.T) {
	result := &pipeline.PolicySimResult{
		PolicySimOutput: &engine.PolicySimOutput{
			Scope:          engine.ScopeSubscription,
			ScopeName:      "sub-1",
			SetTags:        map[string]string{"criticality": "high"},
			TotalResources: 3,
			AffectedCount:  1,
		},
		Impact:  engine.ImpactLow,
		Safety:  "Review in staging first.",
		Summary: "Low blast radius.",
	}
	var buf bytes.Buffer
	if err := RenderPolicySim(&buf, result, FormatJSON); err != nil {
		t.Fatal(err)
	}
	var decoded pipeline.PolicySimResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if decoded.ScopeName != "sub-1" {
		t.Errorf("ScopeName = %q", decoded.ScopeName)
	}
	if decoded.Impact != engine.ImpactLow {
		t.Errorf("Impact = %v", decoded.Impact)
	}
}

func TestRenderPolicySim_tableNoPanic(t *testing.T) {
	result := &pipeline.PolicySimResult{
		PolicySimOutput: &engine.PolicySimOutput{
			Scope:          engine.ScopeResourceGroup,
			ScopeName:      "rg-demo",
			TotalResources: 0,
		},
		BeforeRecsCount: 0,
		AfterRecsCount:  0,
		Impact:          engine.ImpactLow,
		Safety:          "ok",
		Summary:         "test",
	}
	var buf bytes.Buffer
	if err := RenderPolicySim(&buf, result, FormatTable); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty table output")
	}
}

func TestExportPolicySim_markdownSections(t *testing.T) {
	result := &pipeline.PolicySimResult{
		PolicySimOutput: &engine.PolicySimOutput{
			Scope:          engine.ScopeSubscription,
			ScopeName:      "sub-xyz",
			SetTags:        map[string]string{"safecut-mode": "protect"},
			TotalResources: 5,
			AffectedCount:  2,
		},
		BeforeRecsCount: 4,
		AfterRecsCount:  2,
		Impact:          engine.ImpactMedium,
		Safety:          "Preview before apply.",
		Summary:         "Medium blast radius.",
	}
	var buf bytes.Buffer
	if err := ExportPolicySim(&buf, result, "report.md"); err != nil {
		t.Fatalf("export markdown: %v", err)
	}
	out := buf.String()
	for _, section := range []string{
		"# Policy Simulation Report",
		"## Simulation Results",
		"Join the Waitlist",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("markdown missing %q\n---\n%s", section, out)
		}
	}
}
