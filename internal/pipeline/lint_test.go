package pipeline

import (
	"testing"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

func TestDetectInvalidTags_flagsBadMode(t *testing.T) {
	resources := []providers.Resource{{
		ID:   "/sub/rg/Microsoft.Compute/disks/d1",
		Tags: map[string]string{"safecut-mode": "shutdown"},
	}}
	out := detectInvalidTags(resources)
	if len(out) != 1 || out[0].Key != "safecut-mode" {
		t.Fatalf("expected 1 invalid mode tag, got %+v", out)
	}
}

func TestDetectInvalidTags_acceptsValidTemplate(t *testing.T) {
	resources := []providers.Resource{{
		ID:   "/sub/rg/Microsoft.Compute/disks/d1",
		Tags: map[string]string{"safecut-template": "production"},
	}}
	if out := detectInvalidTags(resources); len(out) != 0 {
		t.Fatalf("production template should be valid, got %+v", out)
	}
}

func TestDetectInvalidTags_flagsUnknownTemplate(t *testing.T) {
	resources := []providers.Resource{{
		ID:   "/sub/rg/Microsoft.Compute/disks/d1",
		Tags: map[string]string{"safecut-template": "platinum"},
	}}
	out := detectInvalidTags(resources)
	if len(out) != 1 {
		t.Fatalf("unknown template should be flagged, got %+v", out)
	}
}

func TestDetectInvalidTags_flagsBadExternalBool(t *testing.T) {
	resources := []providers.Resource{{
		ID:   "/sub/rg/Microsoft.Compute/disks/d1",
		Tags: map[string]string{"safecut-external": "maybe"},
	}}
	if out := detectInvalidTags(resources); len(out) != 1 {
		t.Fatalf("bad external bool should be flagged, got %+v", out)
	}
}
