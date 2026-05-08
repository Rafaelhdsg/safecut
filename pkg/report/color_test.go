package report

import (
	"strings"
	"testing"
)

func TestCloudCTA_roiVariant(t *testing.T) {
	SetColor(false)
	defer SetColor(false)
	cta := CloudCTA(500)
	if !strings.Contains(cta, "Solo $29/mo would pay back") {
		t.Fatalf("ROI CTA missing payback line: %q", cta)
	}
	if !strings.Contains(cta, "Lock in founding price") {
		t.Fatalf("ROI CTA missing founding-price copy: %q", cta)
	}
	if !strings.Contains(cta, "#waitlist") && !strings.Contains(cta, "rafaelhdsg.github.io") {
		t.Fatalf("ROI CTA missing conversion URL: %q", cta)
	}
}

func TestCloudCTA_fallbackWhenSavingsLow(t *testing.T) {
	SetColor(false)
	defer SetColor(false)
	cta := CloudCTA(0)
	if !strings.Contains(cta, "Compare plans") {
		t.Fatalf("fallback CTA missing compare plans text: %q", cta)
	}
	if !strings.Contains(cta, "/pricing") {
		t.Fatalf("fallback CTA missing pricing URL: %q", cta)
	}
}

func TestSavings_sign(t *testing.T) {
	SetColor(false)
	if got := Savings(100.0); !strings.Contains(got, "$100.00") {
		t.Errorf("Savings positive: %q", got)
	}
	if got := Savings(0); !strings.Contains(got, "$0.00") {
		t.Errorf("Savings zero: %q", got)
	}
}

func TestSavingsDelta(t *testing.T) {
	SetColor(false)
	if got := SavingsDelta(10); !strings.Contains(got, "+$10.00") {
		t.Errorf("positive delta: %q", got)
	}
	if got := SavingsDelta(-5); !strings.Contains(got, "-$5.00") {
		t.Errorf("negative delta: %q", got)
	}
	if got := SavingsDelta(0); !strings.Contains(got, "$0.00") {
		t.Errorf("zero delta: %q", got)
	}
}
