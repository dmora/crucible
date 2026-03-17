package dialog

import (
	"testing"

	"github.com/dmora/crucible/internal/ui/anim"
)

func TestSpinnerDisplayName_BuiltinPresets(t *testing.T) {
	tests := []struct{ preset, want string }{
		{"industrial", "Industrial"},
		{"pulse", "Pulse"},
		{"dots", "Dots"},
		{"ellipsis", "Ellipsis"},
		{"points", "Points"},
		{"meter", "Meter"},
	}
	for _, tt := range tests {
		if got := SpinnerDisplayName(tt.preset); got != tt.want {
			t.Errorf("SpinnerDisplayName(%q) = %q, want %q", tt.preset, got, tt.want)
		}
	}
}

func TestSpinnerDisplayName_UnknownFallback(t *testing.T) {
	got := SpinnerDisplayName("custom-fast-spin")
	if got != "Custom Fast Spin" {
		t.Errorf("SpinnerDisplayName(custom-fast-spin) = %q, want %q", got, "Custom Fast Spin")
	}
}

func TestSpinnerDisplayName_AllPresetsCovered(t *testing.T) {
	for _, p := range anim.Presets() {
		name := SpinnerDisplayName(p)
		if name == "" {
			t.Errorf("SpinnerDisplayName(%q) returned empty string", p)
		}
	}
}

func TestSpinnerDialogID_NoCollision(t *testing.T) {
	if SpinnerDialogID == "" {
		t.Error("SpinnerDialogID is empty")
	}
	knownIDs := []string{CommandsID, ReasoningID, SessionsID, ModelsID, QuitID, ThemeDialogID}
	for _, id := range knownIDs {
		if SpinnerDialogID == id {
			t.Errorf("SpinnerDialogID %q collides with existing dialog ID", id)
		}
	}
}
