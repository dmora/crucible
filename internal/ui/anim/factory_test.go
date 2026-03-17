package anim

import "testing"

func TestValidPreset_AllPresetsValid(t *testing.T) {
	for _, p := range Presets() {
		if !ValidPreset(p) {
			t.Errorf("ValidPreset(%q) = false, want true", p)
		}
	}
}

func TestValidPreset_UnknownReturnsFalse(t *testing.T) {
	if ValidPreset("fake") {
		t.Error("ValidPreset(fake) should return false")
	}
}

func TestPresets_ContainsAll(t *testing.T) {
	p := Presets()
	if len(p) != 8 {
		t.Fatalf("len(Presets()) = %d, want 8", len(p))
	}
	expected := []string{"industrial", "pulse", "dots", "ellipsis", "points", "meter", "hamburger", "trigram"}
	for i, name := range expected {
		if p[i] != name {
			t.Errorf("Presets()[%d] = %q, want %q", i, p[i], name)
		}
	}
}
