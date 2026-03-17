package dialog

import (
	"testing"

	"github.com/dmora/crucible/internal/ui/styles"
)

func TestThemeDisplayName_BuiltinThemes(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"steel-blue", "Steel Blue"},
		{"amber-forge", "Amber Forge"},
		{"phosphor-green", "Phosphor Green"},
		{"reactor-red", "Reactor Red"},
		{"titanium", "Titanium"},
	}
	for _, tt := range tests {
		got := ThemeDisplayName(tt.id)
		if got != tt.want {
			t.Errorf("ThemeDisplayName(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestThemeDisplayName_UnknownFallback(t *testing.T) {
	got := ThemeDisplayName("custom-dark-neon")
	if got != "Custom Dark Neon" {
		t.Errorf("ThemeDisplayName(custom-dark-neon) = %q, want %q", got, "Custom Dark Neon")
	}
}

func TestThemeDisplayName_AllBuiltinsCovered(t *testing.T) {
	for _, id := range styles.BuiltinThemeIDs() {
		name := ThemeDisplayName(string(id))
		if name == "" {
			t.Errorf("ThemeDisplayName(%q) returned empty string", id)
		}
	}
}

func TestSwatchFromColor(t *testing.T) {
	pal, err := styles.LookupPalette(styles.ThemeSteelBlue)
	if err != nil {
		t.Fatalf("LookupPalette: %v", err)
	}
	swatch := swatchFromColor(pal.Primary)
	if swatch == "" {
		t.Error("swatchFromColor returned empty string")
	}
	// Swatch should contain the block character
	if len(swatch) == 0 {
		t.Error("swatch is empty")
	}
}

func TestThemeDialogID(t *testing.T) {
	if ThemeDialogID == "" {
		t.Error("ThemeDialogID is empty")
	}
	// Must not collide with other dialog IDs
	knownIDs := []string{CommandsID, ReasoningID, SessionsID, ModelsID, QuitID}
	for _, id := range knownIDs {
		if ThemeDialogID == id {
			t.Errorf("ThemeDialogID %q collides with existing dialog ID", id)
		}
	}
}
