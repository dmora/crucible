package styles

import (
	"fmt"
	"image/color"
	"math"
)

// ThemeID identifies a built-in or custom theme.
type ThemeID string

const (
	ThemeSteelBlue     ThemeID = "steel-blue"
	ThemeAmberForge    ThemeID = "amber-forge"
	ThemePhosphorGreen ThemeID = "phosphor-green"
	ThemeReactorRed    ThemeID = "reactor-red"
	ThemeTitanium      ThemeID = "titanium"
	ThemeCleanRoom     ThemeID = "clean-room"

	DefaultTheme = ThemeSteelBlue
)

// Palette is the 4-tier color model that feeds buildStyles.
// Tier 1: Core identity (Primary, Secondary, Tertiary).
// Tier 2: Surfaces (backgrounds and foregrounds).
// Tier 3: Status colors (Error, Warning, Info).
// Tier 4: Named semantic colors used by tools and syntax highlighting.
type Palette struct {
	// Tier 1 — Core identity
	Primary   color.RGBA
	Secondary color.RGBA
	Tertiary  color.RGBA

	// Tier 2 — Surfaces
	BgBase        color.RGBA
	BgBaseLighter color.RGBA
	BgSubtle      color.RGBA
	BgOverlay     color.RGBA

	FgBase      color.RGBA
	FgMuted     color.RGBA
	FgHalfMuted color.RGBA
	FgSubtle    color.RGBA

	Border      color.RGBA
	BorderFocus color.RGBA

	// Tier 3 — Status
	Error   color.RGBA
	Warning color.RGBA
	Info    color.RGBA

	// Tier 4 — Named semantic colors
	White      color.RGBA
	Salt       color.RGBA
	BlueLight  color.RGBA
	Blue       color.RGBA
	BlueDark   color.RGBA
	MedSteel   color.RGBA
	Yellow     color.RGBA
	GreenLight color.RGBA
	Green      color.RGBA
	GreenDark  color.RGBA
	Red        color.RGBA
	RedDark    color.RGBA

	// Syntax highlighting accents
	WarmGray color.RGBA
	Operator color.RGBA

	// Diff colors — inline diff (styles.go InsertLine/DeleteLine)
	DiffInsertFg  color.RGBA
	DiffInsertBg  color.RGBA
	DiffInsertBg2 color.RGBA
	DiffDeleteFg  color.RGBA
	DiffDeleteBg  color.RGBA
	DiffDeleteBg2 color.RGBA
}

// themeRegistry maps theme IDs to their palette constructors.
var themeRegistry = map[ThemeID]func() Palette{
	ThemeSteelBlue:     steelBluePalette,
	ThemeAmberForge:    amberForgePalette,
	ThemePhosphorGreen: phosphorGreenPalette,
	ThemeReactorRed:    reactorRedPalette,
	ThemeTitanium:      titaniumPalette,
	ThemeCleanRoom:     cleanRoomPalette,
}

// BuiltinThemeIDs returns the list of available theme IDs in display order.
func BuiltinThemeIDs() []ThemeID {
	return []ThemeID{
		ThemeSteelBlue,
		ThemeAmberForge,
		ThemePhosphorGreen,
		ThemeReactorRed,
		ThemeTitanium,
		ThemeCleanRoom,
	}
}

// LookupPalette returns the palette for the given theme ID, or an error if not found.
func LookupPalette(id ThemeID) (Palette, error) {
	fn, ok := themeRegistry[id]
	if !ok {
		return Palette{}, fmt.Errorf("unknown theme %q", id)
	}
	return fn(), nil
}

// ValidateThemeID returns true if the given theme ID is a known built-in theme.
func ValidateThemeID(id ThemeID) bool {
	_, ok := themeRegistry[id]
	return ok
}

// ValidatePalette checks that a palette has non-zero core colors.
// Returns an error describing the first invalid field found.
func ValidatePalette(p Palette) error {
	zero := color.RGBA{}
	checks := []struct {
		name  string
		color color.RGBA
	}{
		{"Primary", p.Primary},
		{"Secondary", p.Secondary},
		{"Tertiary", p.Tertiary},
		{"BgBase", p.BgBase},
		{"FgBase", p.FgBase},
		{"Error", p.Error},
		{"Warning", p.Warning},
		{"Green", p.Green},
		{"Red", p.Red},
	}
	for _, c := range checks {
		if c.color == zero {
			return fmt.Errorf("palette field %s is zero-valued", c.name)
		}
	}
	return nil
}

// DeriveDefaults fills in zero-valued Tier 2–4 fields from Tier 1 colors.
// This lets themes define only their core identity and get sensible derived values.
func DeriveDefaults(p Palette) Palette {
	zero := color.RGBA{}

	// Tier 2 surface defaults from BgBase
	isLight := isLightBackground(p.BgBase)

	if p.BgBaseLighter == zero {
		p.BgBaseLighter = surfaceShift(p.BgBase, 0.03, isLight)
	}
	if p.BgSubtle == zero {
		p.BgSubtle = surfaceShift(p.BgBase, 0.07, isLight)
	}
	if p.BgOverlay == zero {
		p.BgOverlay = surfaceShift(p.BgBase, 0.12, isLight)
	}
	if p.FgMuted == zero {
		p.FgMuted = blend(p.FgBase, p.BgBase, 0.5)
	}
	if p.FgHalfMuted == zero {
		p.FgHalfMuted = blend(p.FgBase, p.BgBase, 0.3)
	}
	if p.FgSubtle == zero {
		p.FgSubtle = blend(p.FgBase, p.BgBase, 0.65)
	}
	if p.Border == zero {
		p.Border = p.BgSubtle
	}
	if p.BorderFocus == zero {
		p.BorderFocus = p.Primary
	}

	// Tier 3 defaults
	if p.Info == zero {
		p.Info = p.Primary
	}

	// Tier 4 defaults
	if p.White == zero {
		p.White = color.RGBA{R: 0xDC, G: 0xE0, B: 0xE6, A: 0xFF}
	}
	if p.Salt == zero {
		p.Salt = color.RGBA{R: 0xE8, G: 0xEC, B: 0xF0, A: 0xFF}
	}
	if p.BlueLight == zero {
		p.BlueLight = p.Secondary
	}
	if p.Blue == zero {
		p.Blue = p.Primary
	}
	if p.BlueDark == zero {
		p.BlueDark = p.Tertiary
	}
	if p.MedSteel == zero {
		p.MedSteel = blend(p.Primary, p.Secondary, 0.5)
	}
	if p.Yellow == zero {
		p.Yellow = p.Warning
	}
	if p.GreenLight == zero {
		if isLight {
			p.GreenLight = darken(p.Green, 0.15)
		} else {
			p.GreenLight = lighten(p.Green, 0.15)
		}
	}
	if p.GreenDark == zero {
		p.GreenDark = darken(p.Green, 0.20)
	}
	if p.RedDark == zero {
		p.RedDark = darken(p.Red, 0.25)
	}

	// Syntax accents
	if p.WarmGray == zero {
		p.WarmGray = color.RGBA{R: 0xA0, G: 0x90, B: 0x80, A: 0xFF}
	}
	if p.Operator == zero {
		p.Operator = color.RGBA{R: 0xB8, G: 0x88, B: 0x90, A: 0xFF}
	}

	// Diff defaults
	if p.DiffInsertFg == zero {
		p.DiffInsertFg = p.Green
	}
	if p.DiffInsertBg == zero {
		p.DiffInsertBg = blendWithAlpha(p.Green, p.BgBase, 0.85)
	}
	if p.DiffInsertBg2 == zero {
		p.DiffInsertBg2 = blendWithAlpha(p.Green, p.BgBase, 0.80)
	}
	if p.DiffDeleteFg == zero {
		p.DiffDeleteFg = p.Red
	}
	if p.DiffDeleteBg == zero {
		p.DiffDeleteBg = blendWithAlpha(p.Red, p.BgBase, 0.85)
	}
	if p.DiffDeleteBg2 == zero {
		p.DiffDeleteBg2 = blendWithAlpha(p.Red, p.BgBase, 0.80)
	}

	return p
}

// --- color math helpers ---

func lighten(c color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: clampAdd(c.R, amount),
		G: clampAdd(c.G, amount),
		B: clampAdd(c.B, amount),
		A: c.A,
	}
}

func darken(c color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: clampSub(c.R, amount),
		G: clampSub(c.G, amount),
		B: clampSub(c.B, amount),
		A: c.A,
	}
}

func blend(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t), //nolint:gosec
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t), //nolint:gosec
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t), //nolint:gosec
		A: 0xFF,
	}
}

func blendWithAlpha(fg, bg color.RGBA, bgWeight float64) color.RGBA {
	return blend(fg, bg, bgWeight)
}

func clampAdd(v uint8, amount float64) uint8 {
	result := float64(v) + 255*amount
	if result > 255 {
		return 255
	}
	return uint8(result) //nolint:gosec
}

func clampSub(v uint8, amount float64) uint8 {
	result := float64(v) - 255*amount
	if result < 0 {
		return 0
	}
	return uint8(result) //nolint:gosec
}

// relativeLuminance returns the W3C relative luminance of a color (0.0 = black, 1.0 = white).
func relativeLuminance(c color.RGBA) float64 {
	r := math.Pow(float64(c.R)/255.0, 2.2)
	g := math.Pow(float64(c.G)/255.0, 2.2)
	b := math.Pow(float64(c.B)/255.0, 2.2)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// isLightBackground returns true if the color has relative luminance > 0.5.
func isLightBackground(c color.RGBA) bool {
	return relativeLuminance(c) > 0.5
}

// surfaceShift moves a color away from its base — darker on light bg, lighter on dark bg.
func surfaceShift(base color.RGBA, amount float64, isLight bool) color.RGBA {
	if isLight {
		return darken(base, amount)
	}
	return lighten(base, amount)
}
