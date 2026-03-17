//nolint:dupl // Each palette is intentionally a standalone data declaration with distinct values.
package styles

import "image/color"

// steelBluePalette returns the original DefaultStyles() palette — the reference theme.
// Every hex value here was extracted verbatim from the original DefaultStyles().
func steelBluePalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0x68, G: 0x90, B: 0xB8, A: 0xFF}, // #6890B8
		Secondary: color.RGBA{R: 0x90, G: 0xB8, B: 0xD4, A: 0xFF}, // #90B8D4
		Tertiary:  color.RGBA{R: 0x48, G: 0x70, B: 0xA0, A: 0xFF}, // #4870A0

		// Tier 2 — Surfaces
		BgBase:        color.RGBA{R: 0x18, G: 0x1B, B: 0x20, A: 0xFF}, // #181B20
		BgBaseLighter: color.RGBA{R: 0x20, G: 0x24, B: 0x2A, A: 0xFF}, // #20242A
		BgSubtle:      color.RGBA{R: 0x2A, G: 0x30, B: 0x38, A: 0xFF}, // #2A3038
		BgOverlay:     color.RGBA{R: 0x34, G: 0x3C, B: 0x46, A: 0xFF}, // #343C46

		FgBase:      color.RGBA{R: 0xD4, G: 0xD8, B: 0xE0, A: 0xFF}, // #D4D8E0
		FgMuted:     color.RGBA{R: 0x80, G: 0x88, B: 0x90, A: 0xFF}, // #808890
		FgHalfMuted: color.RGBA{R: 0xA4, G: 0xAC, B: 0xB8, A: 0xFF}, // #A4ACB8
		FgSubtle:    color.RGBA{R: 0x58, G: 0x60, B: 0x68, A: 0xFF}, // #586068

		Border:      color.RGBA{R: 0x2A, G: 0x30, B: 0x38, A: 0xFF}, // #2A3038
		BorderFocus: color.RGBA{R: 0x68, G: 0x90, B: 0xB8, A: 0xFF}, // #6890B8

		// Tier 3 — Status
		Error:   color.RGBA{R: 0xC0, G: 0x48, B: 0x58, A: 0xFF}, // #C04858
		Warning: color.RGBA{R: 0xC4, G: 0xA0, B: 0x48, A: 0xFF}, // #C4A048
		Info:    color.RGBA{R: 0x68, G: 0x90, B: 0xB8, A: 0xFF}, // #6890B8

		// Tier 4 — Named
		White:      color.RGBA{R: 0xDC, G: 0xE0, B: 0xE6, A: 0xFF}, // #DCE0E6
		Salt:       color.RGBA{R: 0xE8, G: 0xEC, B: 0xF0, A: 0xFF}, // #E8ECF0
		BlueLight:  color.RGBA{R: 0x90, G: 0xB8, B: 0xD4, A: 0xFF}, // #90B8D4
		Blue:       color.RGBA{R: 0x68, G: 0x90, B: 0xB8, A: 0xFF}, // #6890B8
		BlueDark:   color.RGBA{R: 0x40, G: 0x58, B: 0x78, A: 0xFF}, // #405878
		MedSteel:   color.RGBA{R: 0x78, G: 0x98, B: 0xB8, A: 0xFF}, // #7898B8
		Yellow:     color.RGBA{R: 0xC4, G: 0xA0, B: 0x48, A: 0xFF}, // #C4A048
		GreenLight: color.RGBA{R: 0x68, G: 0xA8, B: 0x90, A: 0xFF}, // #68A890
		Green:      color.RGBA{R: 0x48, G: 0x90, B: 0x70, A: 0xFF}, // #489070
		GreenDark:  color.RGBA{R: 0x38, G: 0x70, B: 0x58, A: 0xFF}, // #387058
		Red:        color.RGBA{R: 0xC0, G: 0x48, B: 0x58, A: 0xFF}, // #C04858
		RedDark:    color.RGBA{R: 0x90, G: 0x38, B: 0x48, A: 0xFF}, // #903848

		// Syntax accents
		WarmGray: color.RGBA{R: 0xA0, G: 0x90, B: 0x80, A: 0xFF}, // #A09080
		Operator: color.RGBA{R: 0xB8, G: 0x88, B: 0x90, A: 0xFF}, // #B88890

		// Diff — original hardcoded values from DefaultStyles()
		DiffInsertFg:  color.RGBA{R: 0x62, G: 0x96, B: 0x57, A: 0xFF}, // #629657
		DiffInsertBg:  color.RGBA{R: 0x2B, G: 0x32, B: 0x2A, A: 0xFF}, // #2b322a
		DiffInsertBg2: color.RGBA{R: 0x32, G: 0x39, B: 0x31, A: 0xFF}, // #323931
		DiffDeleteFg:  color.RGBA{R: 0xA4, G: 0x5C, B: 0x59, A: 0xFF}, // #a45c59
		DiffDeleteBg:  color.RGBA{R: 0x31, G: 0x29, B: 0x29, A: 0xFF}, // #312929
		DiffDeleteBg2: color.RGBA{R: 0x38, G: 0x30, B: 0x30, A: 0xFF}, // #383030
	}
}

// amberForgePalette — warm amber/gold industrial theme.
func amberForgePalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0xC4, G: 0x90, B: 0x30, A: 0xFF}, // #C49030 amber
		Secondary: color.RGBA{R: 0xD8, G: 0xB0, B: 0x58, A: 0xFF}, // #D8B058 light amber
		Tertiary:  color.RGBA{R: 0x98, G: 0x70, B: 0x28, A: 0xFF}, // #987028 deep amber

		// Tier 2 — warm dark surfaces
		BgBase:        color.RGBA{R: 0x1C, G: 0x18, B: 0x14, A: 0xFF}, // #1C1814
		BgBaseLighter: color.RGBA{R: 0x24, G: 0x20, B: 0x1A, A: 0xFF}, // #24201A
		BgSubtle:      color.RGBA{R: 0x30, G: 0x2A, B: 0x20, A: 0xFF}, // #302A20
		BgOverlay:     color.RGBA{R: 0x3C, G: 0x34, B: 0x28, A: 0xFF}, // #3C3428

		FgBase:      color.RGBA{R: 0xE0, G: 0xD8, B: 0xC8, A: 0xFF}, // #E0D8C8
		FgMuted:     color.RGBA{R: 0x90, G: 0x88, B: 0x78, A: 0xFF}, // #908878
		FgHalfMuted: color.RGBA{R: 0xB8, G: 0xB0, B: 0x98, A: 0xFF}, // #B8B098
		FgSubtle:    color.RGBA{R: 0x68, G: 0x60, B: 0x50, A: 0xFF}, // #686050

		Border:      color.RGBA{R: 0x30, G: 0x2A, B: 0x20, A: 0xFF}, // #302A20
		BorderFocus: color.RGBA{R: 0xC4, G: 0x90, B: 0x30, A: 0xFF}, // #C49030

		// Tier 3
		Error:   color.RGBA{R: 0xC8, G: 0x50, B: 0x40, A: 0xFF}, // #C85040
		Warning: color.RGBA{R: 0xD8, G: 0xB0, B: 0x58, A: 0xFF}, // #D8B058
		Info:    color.RGBA{R: 0xC4, G: 0x90, B: 0x30, A: 0xFF}, // #C49030

		// Tier 4
		White:      color.RGBA{R: 0xE8, G: 0xE0, B: 0xD0, A: 0xFF}, // #E8E0D0
		Salt:       color.RGBA{R: 0xF0, G: 0xE8, B: 0xD8, A: 0xFF}, // #F0E8D8
		BlueLight:  color.RGBA{R: 0xD8, G: 0xB0, B: 0x58, A: 0xFF}, // #D8B058 (secondary)
		Blue:       color.RGBA{R: 0xC4, G: 0x90, B: 0x30, A: 0xFF}, // #C49030 (primary)
		BlueDark:   color.RGBA{R: 0x98, G: 0x70, B: 0x28, A: 0xFF}, // #987028 (tertiary)
		MedSteel:   color.RGBA{R: 0xB0, G: 0x98, B: 0x48, A: 0xFF}, // #B09848
		Yellow:     color.RGBA{R: 0xD8, G: 0xB0, B: 0x58, A: 0xFF}, // #D8B058
		GreenLight: color.RGBA{R: 0x80, G: 0xA8, B: 0x60, A: 0xFF}, // #80A860
		Green:      color.RGBA{R: 0x60, G: 0x90, B: 0x48, A: 0xFF}, // #609048
		GreenDark:  color.RGBA{R: 0x48, G: 0x70, B: 0x38, A: 0xFF}, // #487038
		Red:        color.RGBA{R: 0xC8, G: 0x50, B: 0x40, A: 0xFF}, // #C85040
		RedDark:    color.RGBA{R: 0x98, G: 0x38, B: 0x30, A: 0xFF}, // #983830

		WarmGray: color.RGBA{R: 0xA8, G: 0x98, B: 0x78, A: 0xFF}, // #A89878
		Operator: color.RGBA{R: 0xC0, G: 0x90, B: 0x70, A: 0xFF}, // #C09070

		DiffInsertFg:  color.RGBA{R: 0x60, G: 0x90, B: 0x48, A: 0xFF}, // #609048
		DiffInsertBg:  color.RGBA{R: 0x28, G: 0x30, B: 0x20, A: 0xFF}, // #283020
		DiffInsertBg2: color.RGBA{R: 0x30, G: 0x38, B: 0x28, A: 0xFF}, // #303828
		DiffDeleteFg:  color.RGBA{R: 0xB0, G: 0x58, B: 0x48, A: 0xFF}, // #B05848
		DiffDeleteBg:  color.RGBA{R: 0x30, G: 0x24, B: 0x20, A: 0xFF}, // #302420
		DiffDeleteBg2: color.RGBA{R: 0x38, G: 0x2C, B: 0x28, A: 0xFF}, // #382C28
	}
}

// phosphorGreenPalette — classic CRT green phosphor terminal.
func phosphorGreenPalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0x30, G: 0xB0, B: 0x60, A: 0xFF}, // #30B060
		Secondary: color.RGBA{R: 0x58, G: 0xD0, B: 0x88, A: 0xFF}, // #58D088
		Tertiary:  color.RGBA{R: 0x20, G: 0x88, B: 0x48, A: 0xFF}, // #208848

		// Tier 2
		BgBase:        color.RGBA{R: 0x10, G: 0x18, B: 0x14, A: 0xFF}, // #101814
		BgBaseLighter: color.RGBA{R: 0x18, G: 0x22, B: 0x1C, A: 0xFF}, // #18221C
		BgSubtle:      color.RGBA{R: 0x20, G: 0x2E, B: 0x26, A: 0xFF}, // #202E26
		BgOverlay:     color.RGBA{R: 0x2A, G: 0x3A, B: 0x30, A: 0xFF}, // #2A3A30

		FgBase:      color.RGBA{R: 0xC8, G: 0xE8, B: 0xD0, A: 0xFF}, // #C8E8D0
		FgMuted:     color.RGBA{R: 0x70, G: 0x90, B: 0x78, A: 0xFF}, // #709078
		FgHalfMuted: color.RGBA{R: 0x98, G: 0xB8, B: 0xA0, A: 0xFF}, // #98B8A0
		FgSubtle:    color.RGBA{R: 0x48, G: 0x68, B: 0x50, A: 0xFF}, // #486850

		Border:      color.RGBA{R: 0x20, G: 0x2E, B: 0x26, A: 0xFF}, // #202E26
		BorderFocus: color.RGBA{R: 0x30, G: 0xB0, B: 0x60, A: 0xFF}, // #30B060

		// Tier 3
		Error:   color.RGBA{R: 0xC0, G: 0x50, B: 0x50, A: 0xFF}, // #C05050
		Warning: color.RGBA{R: 0xB8, G: 0xA8, B: 0x40, A: 0xFF}, // #B8A840
		Info:    color.RGBA{R: 0x30, G: 0xB0, B: 0x60, A: 0xFF}, // #30B060

		// Tier 4
		White:      color.RGBA{R: 0xD8, G: 0xF0, B: 0xE0, A: 0xFF}, // #D8F0E0
		Salt:       color.RGBA{R: 0xE0, G: 0xF8, B: 0xE8, A: 0xFF}, // #E0F8E8
		BlueLight:  color.RGBA{R: 0x58, G: 0xD0, B: 0x88, A: 0xFF}, // #58D088
		Blue:       color.RGBA{R: 0x30, G: 0xB0, B: 0x60, A: 0xFF}, // #30B060
		BlueDark:   color.RGBA{R: 0x20, G: 0x88, B: 0x48, A: 0xFF}, // #208848
		MedSteel:   color.RGBA{R: 0x48, G: 0xC0, B: 0x78, A: 0xFF}, // #48C078
		Yellow:     color.RGBA{R: 0xB8, G: 0xA8, B: 0x40, A: 0xFF}, // #B8A840
		GreenLight: color.RGBA{R: 0x68, G: 0xD0, B: 0x90, A: 0xFF}, // #68D090
		Green:      color.RGBA{R: 0x40, G: 0xA8, B: 0x68, A: 0xFF}, // #40A868
		GreenDark:  color.RGBA{R: 0x28, G: 0x80, B: 0x50, A: 0xFF}, // #288050
		Red:        color.RGBA{R: 0xC0, G: 0x50, B: 0x50, A: 0xFF}, // #C05050
		RedDark:    color.RGBA{R: 0x90, G: 0x38, B: 0x38, A: 0xFF}, // #903838

		WarmGray: color.RGBA{R: 0x88, G: 0xA0, B: 0x88, A: 0xFF}, // #88A088
		Operator: color.RGBA{R: 0x90, G: 0xC0, B: 0x90, A: 0xFF}, // #90C090

		DiffInsertFg:  color.RGBA{R: 0x40, G: 0xA8, B: 0x68, A: 0xFF}, // #40A868
		DiffInsertBg:  color.RGBA{R: 0x18, G: 0x28, B: 0x1E, A: 0xFF}, // #18281E
		DiffInsertBg2: color.RGBA{R: 0x20, G: 0x32, B: 0x28, A: 0xFF}, // #203228
		DiffDeleteFg:  color.RGBA{R: 0xB0, G: 0x50, B: 0x50, A: 0xFF}, // #B05050
		DiffDeleteBg:  color.RGBA{R: 0x28, G: 0x1A, B: 0x1A, A: 0xFF}, // #281A1A
		DiffDeleteBg2: color.RGBA{R: 0x30, G: 0x22, B: 0x22, A: 0xFF}, // #302222
	}
}

// reactorRedPalette — warning/reactor red industrial theme.
func reactorRedPalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0xC0, G: 0x40, B: 0x48, A: 0xFF}, // #C04048
		Secondary: color.RGBA{R: 0xD8, G: 0x68, B: 0x70, A: 0xFF}, // #D86870
		Tertiary:  color.RGBA{R: 0x90, G: 0x30, B: 0x38, A: 0xFF}, // #903038

		// Tier 2
		BgBase:        color.RGBA{R: 0x1C, G: 0x14, B: 0x16, A: 0xFF}, // #1C1416
		BgBaseLighter: color.RGBA{R: 0x26, G: 0x1C, B: 0x1E, A: 0xFF}, // #261C1E
		BgSubtle:      color.RGBA{R: 0x32, G: 0x26, B: 0x28, A: 0xFF}, // #322628
		BgOverlay:     color.RGBA{R: 0x40, G: 0x30, B: 0x34, A: 0xFF}, // #403034

		FgBase:      color.RGBA{R: 0xE0, G: 0xD4, B: 0xD6, A: 0xFF}, // #E0D4D6
		FgMuted:     color.RGBA{R: 0x90, G: 0x80, B: 0x84, A: 0xFF}, // #908084
		FgHalfMuted: color.RGBA{R: 0xB8, G: 0xA8, B: 0xAC, A: 0xFF}, // #B8A8AC
		FgSubtle:    color.RGBA{R: 0x68, G: 0x58, B: 0x5C, A: 0xFF}, // #68585C

		Border:      color.RGBA{R: 0x32, G: 0x26, B: 0x28, A: 0xFF}, // #322628
		BorderFocus: color.RGBA{R: 0xC0, G: 0x40, B: 0x48, A: 0xFF}, // #C04048

		// Tier 3
		Error:   color.RGBA{R: 0xD0, G: 0x48, B: 0x48, A: 0xFF}, // #D04848
		Warning: color.RGBA{R: 0xC8, G: 0xA0, B: 0x40, A: 0xFF}, // #C8A040
		Info:    color.RGBA{R: 0xC0, G: 0x40, B: 0x48, A: 0xFF}, // #C04048

		// Tier 4
		White:      color.RGBA{R: 0xE8, G: 0xDC, B: 0xDE, A: 0xFF}, // #E8DCDE
		Salt:       color.RGBA{R: 0xF0, G: 0xE4, B: 0xE8, A: 0xFF}, // #F0E4E8
		BlueLight:  color.RGBA{R: 0xD8, G: 0x68, B: 0x70, A: 0xFF}, // #D86870
		Blue:       color.RGBA{R: 0xC0, G: 0x40, B: 0x48, A: 0xFF}, // #C04048
		BlueDark:   color.RGBA{R: 0x90, G: 0x30, B: 0x38, A: 0xFF}, // #903038
		MedSteel:   color.RGBA{R: 0xC8, G: 0x58, B: 0x60, A: 0xFF}, // #C85860
		Yellow:     color.RGBA{R: 0xC8, G: 0xA0, B: 0x40, A: 0xFF}, // #C8A040
		GreenLight: color.RGBA{R: 0x68, G: 0xA8, B: 0x88, A: 0xFF}, // #68A888
		Green:      color.RGBA{R: 0x48, G: 0x90, B: 0x68, A: 0xFF}, // #489068
		GreenDark:  color.RGBA{R: 0x38, G: 0x70, B: 0x50, A: 0xFF}, // #387050
		Red:        color.RGBA{R: 0xD0, G: 0x48, B: 0x48, A: 0xFF}, // #D04848
		RedDark:    color.RGBA{R: 0x98, G: 0x30, B: 0x30, A: 0xFF}, // #983030

		WarmGray: color.RGBA{R: 0xA0, G: 0x88, B: 0x88, A: 0xFF}, // #A08888
		Operator: color.RGBA{R: 0xC0, G: 0x88, B: 0x88, A: 0xFF}, // #C08888

		DiffInsertFg:  color.RGBA{R: 0x48, G: 0x90, B: 0x68, A: 0xFF}, // #489068
		DiffInsertBg:  color.RGBA{R: 0x20, G: 0x2A, B: 0x24, A: 0xFF}, // #202A24
		DiffInsertBg2: color.RGBA{R: 0x28, G: 0x34, B: 0x2C, A: 0xFF}, // #28342C
		DiffDeleteFg:  color.RGBA{R: 0xC0, G: 0x58, B: 0x50, A: 0xFF}, // #C05850
		DiffDeleteBg:  color.RGBA{R: 0x30, G: 0x20, B: 0x20, A: 0xFF}, // #302020
		DiffDeleteBg2: color.RGBA{R: 0x38, G: 0x28, B: 0x28, A: 0xFF}, // #382828
	}
}

// cleanRoomPalette — light background industrial theme.
// The first non-dark theme: cool off-white with desaturated industrial blue accents.
func cleanRoomPalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0x4A, G: 0x6A, B: 0x8C, A: 0xFF}, // #4A6A8C desaturated industrial blue
		Secondary: color.RGBA{R: 0x6A, G: 0x8A, B: 0xB0, A: 0xFF}, // #6A8AB0 lighter blue accent
		Tertiary:  color.RGBA{R: 0x3A, G: 0x54, B: 0x70, A: 0xFF}, // #3A5470 deeper blue

		// Tier 2 — light surfaces (progressively darker)
		BgBase:        color.RGBA{R: 0xEC, G: 0xEE, B: 0xF2, A: 0xFF}, // #ECEEF2 cool off-white
		BgBaseLighter: color.RGBA{R: 0xE4, G: 0xE6, B: 0xEA, A: 0xFF}, // #E4E6EA
		BgSubtle:      color.RGBA{R: 0xD8, G: 0xDA, B: 0xE0, A: 0xFF}, // #D8DAE0
		BgOverlay:     color.RGBA{R: 0xCC, G: 0xCE, B: 0xD6, A: 0xFF}, // #CCCED6

		FgBase:      color.RGBA{R: 0x2A, G: 0x2D, B: 0x34, A: 0xFF}, // #2A2D34 near-black
		FgMuted:     color.RGBA{R: 0x6A, G: 0x6E, B: 0x78, A: 0xFF}, // #6A6E78
		FgHalfMuted: color.RGBA{R: 0x4A, G: 0x4E, B: 0x58, A: 0xFF}, // #4A4E58
		FgSubtle:    color.RGBA{R: 0x90, G: 0x98, B: 0xA4, A: 0xFF}, // #9098A4

		Border:      color.RGBA{R: 0xC0, G: 0xC4, B: 0xCC, A: 0xFF}, // #C0C4CC
		BorderFocus: color.RGBA{R: 0x4A, G: 0x6A, B: 0x8C, A: 0xFF}, // #4A6A8C

		// Tier 3 — darkened for light-bg contrast
		Error:   color.RGBA{R: 0xA0, G: 0x38, B: 0x40, A: 0xFF}, // #A03840
		Warning: color.RGBA{R: 0xA0, G: 0x78, B: 0x20, A: 0xFF}, // #A07820
		Info:    color.RGBA{R: 0x4A, G: 0x6A, B: 0x8C, A: 0xFF}, // #4A6A8C

		// Tier 4 — adjusted for readability on light bg
		White:      color.RGBA{R: 0xF4, G: 0xF6, B: 0xFA, A: 0xFF}, // #F4F6FA near-white for tag text
		Salt:       color.RGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF}, // #F8FAFC
		BlueLight:  color.RGBA{R: 0x6A, G: 0x8A, B: 0xB0, A: 0xFF}, // #6A8AB0
		Blue:       color.RGBA{R: 0x4A, G: 0x6A, B: 0x8C, A: 0xFF}, // #4A6A8C
		BlueDark:   color.RGBA{R: 0x30, G: 0x48, B: 0x60, A: 0xFF}, // #304860
		MedSteel:   color.RGBA{R: 0x58, G: 0x78, B: 0x9C, A: 0xFF}, // #58789C
		Yellow:     color.RGBA{R: 0xA0, G: 0x78, B: 0x20, A: 0xFF}, // #A07820
		GreenLight: color.RGBA{R: 0x30, G: 0x70, B: 0x50, A: 0xFF}, // #307050 darkened for light bg
		Green:      color.RGBA{R: 0x38, G: 0x80, B: 0x5C, A: 0xFF}, // #38805C
		GreenDark:  color.RGBA{R: 0x28, G: 0x60, B: 0x44, A: 0xFF}, // #286044
		Red:        color.RGBA{R: 0xA0, G: 0x38, B: 0x40, A: 0xFF}, // #A03840
		RedDark:    color.RGBA{R: 0x80, G: 0x28, B: 0x30, A: 0xFF}, // #802830

		// Syntax accents — darkened for visibility on light bg
		WarmGray: color.RGBA{R: 0x70, G: 0x68, B: 0x60, A: 0xFF}, // #706860
		Operator: color.RGBA{R: 0x88, G: 0x60, B: 0x68, A: 0xFF}, // #886068

		// Diff — light-adapted tints
		DiffInsertFg:  color.RGBA{R: 0x38, G: 0x78, B: 0x50, A: 0xFF}, // #387850
		DiffInsertBg:  color.RGBA{R: 0xD8, G: 0xEC, B: 0xD8, A: 0xFF}, // #D8ECD8
		DiffInsertBg2: color.RGBA{R: 0xC8, G: 0xE4, B: 0xC8, A: 0xFF}, // #C8E4C8
		DiffDeleteFg:  color.RGBA{R: 0x90, G: 0x40, B: 0x40, A: 0xFF}, // #904040
		DiffDeleteBg:  color.RGBA{R: 0xF0, G: 0xD8, B: 0xD8, A: 0xFF}, // #F0D8D8
		DiffDeleteBg2: color.RGBA{R: 0xE8, G: 0xCC, B: 0xCC, A: 0xFF}, // #E8CCCC
	}
}

// titaniumPalette — neutral monochrome with subtle cool tones.
func titaniumPalette() Palette {
	return Palette{
		// Tier 1
		Primary:   color.RGBA{R: 0x88, G: 0x90, B: 0x98, A: 0xFF}, // #889098
		Secondary: color.RGBA{R: 0xA8, G: 0xB0, B: 0xB8, A: 0xFF}, // #A8B0B8
		Tertiary:  color.RGBA{R: 0x68, G: 0x70, B: 0x78, A: 0xFF}, // #687078

		// Tier 2
		BgBase:        color.RGBA{R: 0x18, G: 0x1A, B: 0x1C, A: 0xFF}, // #181A1C
		BgBaseLighter: color.RGBA{R: 0x22, G: 0x24, B: 0x26, A: 0xFF}, // #222426
		BgSubtle:      color.RGBA{R: 0x2C, G: 0x2E, B: 0x32, A: 0xFF}, // #2C2E32
		BgOverlay:     color.RGBA{R: 0x38, G: 0x3A, B: 0x40, A: 0xFF}, // #383A40

		FgBase:      color.RGBA{R: 0xD8, G: 0xDA, B: 0xDE, A: 0xFF}, // #D8DADE
		FgMuted:     color.RGBA{R: 0x88, G: 0x8A, B: 0x90, A: 0xFF}, // #888A90
		FgHalfMuted: color.RGBA{R: 0xA8, G: 0xAC, B: 0xB2, A: 0xFF}, // #A8ACB2
		FgSubtle:    color.RGBA{R: 0x5C, G: 0x5E, B: 0x64, A: 0xFF}, // #5C5E64

		Border:      color.RGBA{R: 0x2C, G: 0x2E, B: 0x32, A: 0xFF}, // #2C2E32
		BorderFocus: color.RGBA{R: 0x88, G: 0x90, B: 0x98, A: 0xFF}, // #889098

		// Tier 3
		Error:   color.RGBA{R: 0xC0, G: 0x50, B: 0x58, A: 0xFF}, // #C05058
		Warning: color.RGBA{R: 0xC0, G: 0xA0, B: 0x48, A: 0xFF}, // #C0A048
		Info:    color.RGBA{R: 0x88, G: 0x90, B: 0x98, A: 0xFF}, // #889098

		// Tier 4
		White:      color.RGBA{R: 0xE0, G: 0xE2, B: 0xE6, A: 0xFF}, // #E0E2E6
		Salt:       color.RGBA{R: 0xEA, G: 0xEC, B: 0xF0, A: 0xFF}, // #EAECF0
		BlueLight:  color.RGBA{R: 0xA8, G: 0xB0, B: 0xB8, A: 0xFF}, // #A8B0B8
		Blue:       color.RGBA{R: 0x88, G: 0x90, B: 0x98, A: 0xFF}, // #889098
		BlueDark:   color.RGBA{R: 0x68, G: 0x70, B: 0x78, A: 0xFF}, // #687078
		MedSteel:   color.RGBA{R: 0x98, G: 0xA0, B: 0xA8, A: 0xFF}, // #98A0A8
		Yellow:     color.RGBA{R: 0xC0, G: 0xA0, B: 0x48, A: 0xFF}, // #C0A048
		GreenLight: color.RGBA{R: 0x68, G: 0xA0, B: 0x88, A: 0xFF}, // #68A088
		Green:      color.RGBA{R: 0x48, G: 0x88, B: 0x70, A: 0xFF}, // #488870
		GreenDark:  color.RGBA{R: 0x38, G: 0x68, B: 0x58, A: 0xFF}, // #386858
		Red:        color.RGBA{R: 0xC0, G: 0x50, B: 0x58, A: 0xFF}, // #C05058
		RedDark:    color.RGBA{R: 0x90, G: 0x38, B: 0x40, A: 0xFF}, // #903840

		WarmGray: color.RGBA{R: 0x98, G: 0x90, B: 0x88, A: 0xFF}, // #989088
		Operator: color.RGBA{R: 0xA8, G: 0x90, B: 0x90, A: 0xFF}, // #A89090

		DiffInsertFg:  color.RGBA{R: 0x58, G: 0x90, B: 0x70, A: 0xFF}, // #589070
		DiffInsertBg:  color.RGBA{R: 0x22, G: 0x2C, B: 0x28, A: 0xFF}, // #222C28
		DiffInsertBg2: color.RGBA{R: 0x28, G: 0x34, B: 0x2E, A: 0xFF}, // #28342E
		DiffDeleteFg:  color.RGBA{R: 0xA8, G: 0x58, B: 0x58, A: 0xFF}, // #A85858
		DiffDeleteBg:  color.RGBA{R: 0x2C, G: 0x22, B: 0x22, A: 0xFF}, // #2C2222
		DiffDeleteBg2: color.RGBA{R: 0x34, G: 0x28, B: 0x28, A: 0xFF}, // #342828
	}
}
