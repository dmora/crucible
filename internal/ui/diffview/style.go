package diffview

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// LineStyle defines the styles for a given line type in the diff view.
type LineStyle struct {
	LineNumber lipgloss.Style
	Symbol     lipgloss.Style
	Code       lipgloss.Style
}

// Style defines the overall style for the diff view, including styles for
// different line types such as divider, missing, equal, insert, and delete
// lines.
type Style struct {
	DividerLine LineStyle
	MissingLine LineStyle
	EqualLine   LineStyle
	InsertLine  LineStyle
	DeleteLine  LineStyle
}

// Crucible Industrial FUI diff colors.
var (
	fgBase    = color.RGBA{R: 0xD4, G: 0xD8, B: 0xE0, A: 0xFF} // #D4D8E0
	fgMuted   = color.RGBA{R: 0xA4, G: 0xAC, B: 0xB8, A: 0xFF} // #A4ACB8
	fgSubtle  = color.RGBA{R: 0x58, G: 0x60, B: 0x68, A: 0xFF} // #586068
	bgBase    = color.RGBA{R: 0x18, G: 0x1B, B: 0x20, A: 0xFF} // #181B20
	bgSubtle  = color.RGBA{R: 0x2A, G: 0x30, B: 0x38, A: 0xFF} // #2A3038
	bgOverlay = color.RGBA{R: 0x34, G: 0x3C, B: 0x46, A: 0xFF} // #343C46
	salt      = color.RGBA{R: 0xE8, G: 0xEC, B: 0xF0, A: 0xFF} // #E8ECF0
	steelBlue = color.RGBA{R: 0x40, G: 0x58, B: 0x78, A: 0xFF} // #405878
	teal      = color.RGBA{R: 0x50, G: 0x88, B: 0x88, A: 0xFF} // #508888
	mutedRed  = color.RGBA{R: 0xC0, G: 0x48, B: 0x58, A: 0xFF} // #C04858
)

// DefaultLightStyle provides a default light theme style for the diff view.
func DefaultLightStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(bgOverlay).
				Background(steelBlue),
			Code: lipgloss.NewStyle().
				Foreground(fgSubtle).
				Background(fgMuted),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(fgBase),
			Code: lipgloss.NewStyle().
				Background(fgBase),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(bgSubtle).
				Background(fgBase),
			Code: lipgloss.NewStyle().
				Foreground(bgBase).
				Background(salt),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(teal).
				Background(lipgloss.Color("#c8e6c9")),
			Symbol: lipgloss.NewStyle().
				Foreground(teal).
				Background(lipgloss.Color("#e8f5e9")),
			Code: lipgloss.NewStyle().
				Foreground(bgBase).
				Background(lipgloss.Color("#e8f5e9")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(mutedRed).
				Background(lipgloss.Color("#ffcdd2")),
			Symbol: lipgloss.NewStyle().
				Foreground(mutedRed).
				Background(lipgloss.Color("#ffebee")),
			Code: lipgloss.NewStyle().
				Foreground(bgBase).
				Background(lipgloss.Color("#ffebee")),
		},
	}
}

// DefaultDarkStyle provides a default dark theme style for the diff view.
func DefaultDarkStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(fgMuted).
				Background(steelBlue),
			Code: lipgloss.NewStyle().
				Foreground(fgMuted).
				Background(bgOverlay),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(bgSubtle),
			Code: lipgloss.NewStyle().
				Background(bgSubtle),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(fgBase).
				Background(bgSubtle),
			Code: lipgloss.NewStyle().
				Foreground(salt).
				Background(bgBase),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(teal).
				Background(lipgloss.Color("#1E2A28")),
			Symbol: lipgloss.NewStyle().
				Foreground(teal).
				Background(lipgloss.Color("#243030")),
			Code: lipgloss.NewStyle().
				Foreground(salt).
				Background(lipgloss.Color("#243030")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(mutedRed).
				Background(lipgloss.Color("#2A1E22")),
			Symbol: lipgloss.NewStyle().
				Foreground(mutedRed).
				Background(lipgloss.Color("#302428")),
			Code: lipgloss.NewStyle().
				Foreground(salt).
				Background(lipgloss.Color("#302428")),
		},
	}
}
