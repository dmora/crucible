package styles

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBuiltinThemeIDs(t *testing.T) {
	ids := BuiltinThemeIDs()
	if len(ids) != 6 {
		t.Fatalf("expected 6 built-in themes, got %d", len(ids))
	}
	// Verify exact order
	expected := []ThemeID{
		ThemeSteelBlue,
		ThemeAmberForge,
		ThemePhosphorGreen,
		ThemeReactorRed,
		ThemeTitanium,
		ThemeCleanRoom,
	}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("theme[%d]: got %q, want %q", i, id, expected[i])
		}
	}
}

func TestValidateThemeID(t *testing.T) {
	for _, id := range BuiltinThemeIDs() {
		if !ValidateThemeID(id) {
			t.Errorf("ValidateThemeID(%q) = false, want true", id)
		}
	}
	if ValidateThemeID("nonexistent") {
		t.Error("ValidateThemeID(nonexistent) = true, want false")
	}
	if ValidateThemeID("") {
		t.Error("ValidateThemeID(\"\") = true, want false")
	}
}

func TestLookupPalette_AllBuiltins(t *testing.T) {
	for _, id := range BuiltinThemeIDs() {
		p, err := LookupPalette(id)
		if err != nil {
			t.Fatalf("LookupPalette(%q): %v", id, err)
		}
		if err := ValidatePalette(p); err != nil {
			t.Errorf("ValidatePalette(%q): %v", id, err)
		}
	}
}

func TestLookupPalette_Unknown(t *testing.T) {
	_, err := LookupPalette("does-not-exist")
	if err == nil {
		t.Error("expected error for unknown theme, got nil")
	}
}

func TestValidatePalette_ZeroFields(t *testing.T) {
	// Completely zero palette should fail
	err := ValidatePalette(Palette{})
	if err == nil {
		t.Error("expected error for zero palette, got nil")
	}

	// Palette missing just Error should fail
	partial := steelBluePalette()
	partial.Error = color.RGBA{}
	if err := ValidatePalette(partial); err == nil {
		t.Error("expected error for palette with zero Error, got nil")
	}
}

func TestSteelBluePalette_MatchesOriginalColors(t *testing.T) {
	// Verify the steel-blue palette exactly matches the original DefaultStyles() hardcoded colors.
	p := steelBluePalette()

	assertColor(t, "Primary", p.Primary, 0x68, 0x90, 0xB8)
	assertColor(t, "Secondary", p.Secondary, 0x90, 0xB8, 0xD4)
	assertColor(t, "Tertiary", p.Tertiary, 0x48, 0x70, 0xA0)
	assertColor(t, "BgBase", p.BgBase, 0x18, 0x1B, 0x20)
	assertColor(t, "BgBaseLighter", p.BgBaseLighter, 0x20, 0x24, 0x2A)
	assertColor(t, "BgSubtle", p.BgSubtle, 0x2A, 0x30, 0x38)
	assertColor(t, "BgOverlay", p.BgOverlay, 0x34, 0x3C, 0x46)
	assertColor(t, "FgBase", p.FgBase, 0xD4, 0xD8, 0xE0)
	assertColor(t, "FgMuted", p.FgMuted, 0x80, 0x88, 0x90)
	assertColor(t, "FgHalfMuted", p.FgHalfMuted, 0xA4, 0xAC, 0xB8)
	assertColor(t, "FgSubtle", p.FgSubtle, 0x58, 0x60, 0x68)
	assertColor(t, "Error", p.Error, 0xC0, 0x48, 0x58)
	assertColor(t, "Warning", p.Warning, 0xC4, 0xA0, 0x48)
	assertColor(t, "Green", p.Green, 0x48, 0x90, 0x70)
	assertColor(t, "Red", p.Red, 0xC0, 0x48, 0x58)
	assertColor(t, "DiffInsertFg", p.DiffInsertFg, 0x62, 0x96, 0x57)
	assertColor(t, "DiffDeleteFg", p.DiffDeleteFg, 0xA4, 0x5C, 0x59)
}

func TestNewStyles_DefaultTheme(t *testing.T) {
	s := NewStyles(DefaultTheme, false)
	// Verify core semantic colors match steel-blue palette
	assertColorColor(t, "Primary", s.Primary, steelBluePalette().Primary)
	assertColorColor(t, "Secondary", s.Secondary, steelBluePalette().Secondary)
	assertColorColor(t, "BgBase", s.BgBase, steelBluePalette().BgBase)
	assertColorColor(t, "Error", s.Error, steelBluePalette().Error)
}

func TestNewStyles_FallsBackOnUnknown(t *testing.T) {
	s := NewStyles("bogus-theme", false)
	// Should fall back to steel-blue
	assertColorColor(t, "Primary", s.Primary, steelBluePalette().Primary)
}

func TestDefaultStyles_BackwardCompat(t *testing.T) {
	old := DefaultStyles()
	fromNew := NewStyles(DefaultTheme, false)
	// Same primary
	if colorToRGBA(old.Primary) != colorToRGBA(fromNew.Primary) {
		t.Errorf("Primary mismatch between DefaultStyles and NewStyles(DefaultTheme)")
	}
	if colorToRGBA(old.BgBase) != colorToRGBA(fromNew.BgBase) {
		t.Errorf("BgBase mismatch between DefaultStyles and NewStyles(DefaultTheme)")
	}
}

func TestNewStyles_AllThemesProduce_DistinctPrimaries(t *testing.T) {
	primaries := make(map[color.RGBA]ThemeID)
	for _, id := range BuiltinThemeIDs() {
		s := NewStyles(id, false)
		rgba := colorToRGBA(s.Primary)
		if existing, ok := primaries[rgba]; ok {
			t.Errorf("themes %q and %q share primary color #%02X%02X%02X",
				existing, id, rgba.R, rgba.G, rgba.B)
		}
		primaries[rgba] = id
	}
}

func TestNewStyles_AllThemesPopulateKeyFields(t *testing.T) {
	for _, id := range BuiltinThemeIDs() {
		s := NewStyles(id, false)
		zero := color.RGBA{}

		if colorToRGBA(s.Primary) == zero {
			t.Errorf("%s: Primary is zero", id)
		}
		if colorToRGBA(s.BgBase) == zero {
			t.Errorf("%s: BgBase is zero", id)
		}
		if colorToRGBA(s.FgBase) == zero {
			t.Errorf("%s: FgBase is zero", id)
		}
		if colorToRGBA(s.Error) == zero {
			t.Errorf("%s: Error is zero", id)
		}
		if colorToRGBA(s.Green) == zero {
			t.Errorf("%s: Green is zero", id)
		}
		if colorToRGBA(s.Red) == zero {
			t.Errorf("%s: Red is zero", id)
		}
	}
}

func TestNewStyles_HoldEditorStylesPopulated(t *testing.T) {
	for _, id := range BuiltinThemeIDs() {
		s := NewStyles(id, false)
		if s.EditorPromptHoldIconFocused.Render() == "" {
			t.Errorf("%s: EditorPromptHoldIconFocused renders empty", id)
		}
		if s.EditorPromptHoldIconBlurred.Render() == "" {
			t.Errorf("%s: EditorPromptHoldIconBlurred renders empty", id)
		}
		if s.EditorPromptHoldDotsFocused.Render() == "" {
			t.Errorf("%s: EditorPromptHoldDotsFocused renders empty", id)
		}
		if s.EditorPromptHoldDotsBlurred.Render() == "" {
			t.Errorf("%s: EditorPromptHoldDotsBlurred renders empty", id)
		}
	}
}

func TestDeriveDefaults_FillsZeroFields(t *testing.T) {
	// Minimal palette with only Tier 1 + BgBase + FgBase + status + green + red
	p := Palette{
		Primary:   color.RGBA{R: 0x60, G: 0x80, B: 0xA0, A: 0xFF},
		Secondary: color.RGBA{R: 0x80, G: 0xA0, B: 0xC0, A: 0xFF},
		Tertiary:  color.RGBA{R: 0x40, G: 0x60, B: 0x80, A: 0xFF},
		BgBase:    color.RGBA{R: 0x18, G: 0x1B, B: 0x20, A: 0xFF},
		FgBase:    color.RGBA{R: 0xD0, G: 0xD4, B: 0xD8, A: 0xFF},
		Error:     color.RGBA{R: 0xC0, G: 0x40, B: 0x50, A: 0xFF},
		Warning:   color.RGBA{R: 0xC0, G: 0xA0, B: 0x40, A: 0xFF},
		Green:     color.RGBA{R: 0x48, G: 0x90, B: 0x70, A: 0xFF},
		Red:       color.RGBA{R: 0xC0, G: 0x40, B: 0x50, A: 0xFF},
	}

	derived := DeriveDefaults(p)

	zero := color.RGBA{}
	if derived.BgBaseLighter == zero {
		t.Error("BgBaseLighter not derived")
	}
	if derived.BgSubtle == zero {
		t.Error("BgSubtle not derived")
	}
	if derived.BgOverlay == zero {
		t.Error("BgOverlay not derived")
	}
	if derived.FgMuted == zero {
		t.Error("FgMuted not derived")
	}
	if derived.FgHalfMuted == zero {
		t.Error("FgHalfMuted not derived")
	}
	if derived.FgSubtle == zero {
		t.Error("FgSubtle not derived")
	}
	if derived.Border == zero {
		t.Error("Border not derived")
	}
	if derived.BorderFocus == zero {
		t.Error("BorderFocus not derived")
	}
	if derived.Info == zero {
		t.Error("Info not derived")
	}
	if derived.White == zero {
		t.Error("White not derived")
	}
	if derived.GreenLight == zero {
		t.Error("GreenLight not derived")
	}
	if derived.GreenDark == zero {
		t.Error("GreenDark not derived")
	}
	if derived.RedDark == zero {
		t.Error("RedDark not derived")
	}
	if derived.DiffInsertFg == zero {
		t.Error("DiffInsertFg not derived")
	}
	if derived.DiffDeleteFg == zero {
		t.Error("DiffDeleteFg not derived")
	}
}

func TestDeriveDefaults_DoesNotOverrideExplicit(t *testing.T) {
	p := steelBluePalette()
	original := p.BgBaseLighter
	derived := DeriveDefaults(p)
	if derived.BgBaseLighter != original {
		t.Errorf("DeriveDefaults overrode explicit BgBaseLighter: got %v, want %v",
			derived.BgBaseLighter, original)
	}
}

// --- new tests for clean-room, light bg, and transparent ---

func TestCleanRoomPalette_Valid(t *testing.T) {
	p := cleanRoomPalette()
	if err := ValidatePalette(p); err != nil {
		t.Errorf("cleanRoomPalette() failed validation: %v", err)
	}
}

func TestIsLightBackground(t *testing.T) {
	// clean-room BgBase (#ECEEF2) should be light
	if !isLightBackground(color.RGBA{R: 0xEC, G: 0xEE, B: 0xF2, A: 0xFF}) {
		t.Error("isLightBackground(#ECEEF2) = false, want true")
	}
	// steel-blue BgBase (#181B20) should be dark
	if isLightBackground(color.RGBA{R: 0x18, G: 0x1B, B: 0x20, A: 0xFF}) {
		t.Error("isLightBackground(#181B20) = true, want false")
	}
}

func TestDeriveDefaults_LightBg_DarkensInsteadOfLightens(t *testing.T) {
	// Minimal palette with a light BgBase
	p := Palette{
		Primary:   color.RGBA{R: 0x4A, G: 0x6A, B: 0x8C, A: 0xFF},
		Secondary: color.RGBA{R: 0x6A, G: 0x8A, B: 0xB0, A: 0xFF},
		Tertiary:  color.RGBA{R: 0x3A, G: 0x54, B: 0x70, A: 0xFF},
		BgBase:    color.RGBA{R: 0xEC, G: 0xEE, B: 0xF2, A: 0xFF}, // light
		FgBase:    color.RGBA{R: 0x2A, G: 0x2D, B: 0x34, A: 0xFF},
		Error:     color.RGBA{R: 0xA0, G: 0x38, B: 0x40, A: 0xFF},
		Warning:   color.RGBA{R: 0xA0, G: 0x78, B: 0x20, A: 0xFF},
		Green:     color.RGBA{R: 0x38, G: 0x80, B: 0x5C, A: 0xFF},
		Red:       color.RGBA{R: 0xA0, G: 0x38, B: 0x40, A: 0xFF},
	}

	derived := DeriveDefaults(p)

	// On light bg, BgSubtle should be darker (lower RGB sum) than BgBase
	bgSum := int(p.BgBase.R) + int(p.BgBase.G) + int(p.BgBase.B)
	subtleSum := int(derived.BgSubtle.R) + int(derived.BgSubtle.G) + int(derived.BgSubtle.B)
	if subtleSum >= bgSum {
		t.Errorf("light bg: BgSubtle (%d) should be darker than BgBase (%d)", subtleSum, bgSum)
	}
}

func TestDeriveDefaults_DarkBg_StillLightens(t *testing.T) {
	// Existing dark palette — regression guard
	p := Palette{
		Primary:   color.RGBA{R: 0x60, G: 0x80, B: 0xA0, A: 0xFF},
		Secondary: color.RGBA{R: 0x80, G: 0xA0, B: 0xC0, A: 0xFF},
		Tertiary:  color.RGBA{R: 0x40, G: 0x60, B: 0x80, A: 0xFF},
		BgBase:    color.RGBA{R: 0x18, G: 0x1B, B: 0x20, A: 0xFF}, // dark
		FgBase:    color.RGBA{R: 0xD0, G: 0xD4, B: 0xD8, A: 0xFF},
		Error:     color.RGBA{R: 0xC0, G: 0x40, B: 0x50, A: 0xFF},
		Warning:   color.RGBA{R: 0xC0, G: 0xA0, B: 0x40, A: 0xFF},
		Green:     color.RGBA{R: 0x48, G: 0x90, B: 0x70, A: 0xFF},
		Red:       color.RGBA{R: 0xC0, G: 0x40, B: 0x50, A: 0xFF},
	}

	derived := DeriveDefaults(p)

	// On dark bg, BgSubtle should be lighter (higher RGB sum) than BgBase
	bgSum := int(p.BgBase.R) + int(p.BgBase.G) + int(p.BgBase.B)
	subtleSum := int(derived.BgSubtle.R) + int(derived.BgSubtle.G) + int(derived.BgSubtle.B)
	if subtleSum <= bgSum {
		t.Errorf("dark bg: BgSubtle (%d) should be lighter than BgBase (%d)", subtleSum, bgSum)
	}
}

func isNoBg(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return ok || c == nil
}

func TestNewStyles_TransparentStripsContentBg(t *testing.T) {
	s := NewStyles(DefaultTheme, true)
	if !isNoBg(s.PanelMuted.GetBackground()) {
		t.Error("transparent: PanelMuted should have no background")
	}
	if !isNoBg(s.Tool.ContentLine.GetBackground()) {
		t.Error("transparent: Tool.ContentLine should have no background")
	}
	if s.Tool.ContentCodeBg != nil {
		t.Error("transparent: Tool.ContentCodeBg should be nil")
	}
}

func TestNewStyles_TransparentKeepsAccentBg(t *testing.T) {
	s := NewStyles(DefaultTheme, true)
	if isNoBg(s.Tool.ErrorTag.GetBackground()) {
		t.Error("transparent: Tool.ErrorTag should keep its accent background")
	}
}

func TestNewStyles_OpaqueKeepsContentBg(t *testing.T) {
	s := NewStyles(DefaultTheme, false)
	if isNoBg(s.PanelMuted.GetBackground()) {
		t.Error("opaque: PanelMuted should have a background")
	}
	if s.Tool.ContentCodeBg == nil {
		t.Error("opaque: Tool.ContentCodeBg should not be nil")
	}
}

func TestNewStyles_CleanRoom(t *testing.T) {
	s := NewStyles(ThemeCleanRoom, false)
	zero := color.RGBA{}
	if colorToRGBA(s.Primary) == zero {
		t.Error("clean-room: Primary is zero")
	}
	if colorToRGBA(s.BgBase) == zero {
		t.Error("clean-room: BgBase is zero")
	}
	if colorToRGBA(s.FgBase) == zero {
		t.Error("clean-room: FgBase is zero")
	}
	if colorToRGBA(s.Error) == zero {
		t.Error("clean-room: Error is zero")
	}
	if colorToRGBA(s.Green) == zero {
		t.Error("clean-room: Green is zero")
	}
	if colorToRGBA(s.Red) == zero {
		t.Error("clean-room: Red is zero")
	}
}

// --- helpers ---

func assertColor(t *testing.T, name string, got color.RGBA, wantR, wantG, wantB uint8) {
	t.Helper()
	if got.R != wantR || got.G != wantG || got.B != wantB {
		t.Errorf("%s: got #%02X%02X%02X, want #%02X%02X%02X",
			name, got.R, got.G, got.B, wantR, wantG, wantB)
	}
}

func assertColorColor(t *testing.T, name string, got color.Color, want color.RGBA) {
	t.Helper()
	rgba := colorToRGBA(got)
	if rgba != want {
		t.Errorf("%s: got #%02X%02X%02X, want #%02X%02X%02X",
			name, rgba.R, rgba.G, rgba.B, want.R, want.G, want.B)
	}
}

func colorToRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{
		R: uint8(r >> 8), //nolint:gosec
		G: uint8(g >> 8), //nolint:gosec
		B: uint8(b >> 8), //nolint:gosec
		A: uint8(a >> 8), //nolint:gosec
	}
}
