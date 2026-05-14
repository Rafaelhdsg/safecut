package engine

import "testing"

func TestPolicyResolver_inheritsFromRG(t *testing.T) {
	r := NewPolicyResolver()
	h := TagHierarchy{
		ResourceGroupTags: map[string]string{"safecut-mode": "protect"},
		ResourceGroupName: "rg-prod",
	}
	rp := r.Resolve(h)
	if rp.Mode != ModeProtect {
		t.Fatalf("expected inherited protect, got %q", rp.Mode)
	}
	if rp.ModeOrigin.Source != SourceResourceGroup {
		t.Errorf("expected origin RG, got %q", rp.ModeOrigin.Source)
	}
}

func TestPolicyResolver_resourceOverridesRG(t *testing.T) {
	r := NewPolicyResolver()
	h := TagHierarchy{
		ResourceTags:      map[string]string{"safecut-mode": "ignore"},
		ResourceGroupTags: map[string]string{"safecut-mode": "protect"},
	}
	rp := r.Resolve(h)
	if rp.Mode != ModeIgnore {
		t.Fatalf("expected resource-level ignore to win, got %q", rp.Mode)
	}
	if rp.ModeOrigin.Source != SourceResource {
		t.Errorf("expected origin Resource, got %q", rp.ModeOrigin.Source)
	}
}

func TestPolicyResolver_templateProduction(t *testing.T) {
	r := NewPolicyResolver()
	h := TagHierarchy{
		ResourceTags: map[string]string{"safecut-template": "production"},
	}
	rp := r.Resolve(h)
	if rp.Mode != ModeProtect || rp.Criticality != CriticalityHigh {
		t.Fatalf("production template should map protect/high, got %+v", rp)
	}
}

func TestPolicyResolver_detectsDrift(t *testing.T) {
	r := NewPolicyResolver()
	h := TagHierarchy{
		ResourceTags:      map[string]string{"safecut-mode": "ignore"},
		ResourceGroupTags: map[string]string{"safecut-mode": "protect"},
		ResourceGroupName: "rg-prod",
	}
	rp := r.Resolve(h)
	if len(rp.Drifts) == 0 {
		t.Fatalf("expected drift between resource ignore vs RG protect")
	}
	found := false
	for _, d := range rp.Drifts {
		if d.Field == "mode" && d.ResourceValue == "ignore" && d.ParentValue == "protect" {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not find expected mode drift in %+v", rp.Drifts)
	}
}

func TestPolicyResolver_overrideDisablesInheritance(t *testing.T) {
	r := NewPolicyResolver()
	h := TagHierarchy{
		ResourceTags: map[string]string{
			"safecut-policy": "override",
			"safecut-mode":   "ignore",
		},
		ResourceGroupTags: map[string]string{"safecut-mode": "protect"},
	}
	rp := r.Resolve(h)
	if rp.Mode != ModeIgnore {
		t.Fatalf("expected explicit ignore with override flag, got %q", rp.Mode)
	}
	if len(rp.Drifts) != 0 {
		t.Fatalf("override suppresses drift reporting, got %+v", rp.Drifts)
	}
}

func TestPolicyResolver_defaults(t *testing.T) {
	r := NewPolicyResolver()
	rp := r.Resolve(TagHierarchy{})
	if rp.Mode != ModeDefault {
		t.Errorf("Mode default = %q", rp.Mode)
	}
	if rp.ModeOrigin.Source != SourceDefault {
		t.Errorf("origin default = %q", rp.ModeOrigin.Source)
	}
}
