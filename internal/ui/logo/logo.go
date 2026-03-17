// Package logo renders a Crucible wordmark in a stylized way.
package logo

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MakeNowJust/heredoc"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/slice"
	"github.com/dmora/crucible/internal/ui/styles"
)

// letterform represents a letterform. It can be stretched horizontally by
// a given amount via the boolean argument.
type letterform func(bool) string

const fieldStride = 2 // cells per repeating unit: 1 slash + 1 space

// Opts are the options for rendering the Crucible title art.
type Opts struct {
	FieldColor   color.Color // diagonal lines
	StrokeColor  color.Color // foreground for field strokes (defaults to FieldColor dimmed)
	TitleColorA  color.Color // left gradient ramp point
	TitleColorB  color.Color // right gradient ramp point
	VersionColor color.Color // Version text color
	Width        int         // width of the rendered logo, used for truncation
}

// Render renders the Crucible logo. Set the argument to true to render the narrow
// version, intended for use in a sidebar.
//
// The compact argument determines whether it renders compact for the sidebar
// or wider for the main pane.
func Render(s *styles.Styles, version string, compact bool, o Opts) string {
	if o.StrokeColor == nil {
		o.StrokeColor = dimColor(o.FieldColor, 0.70)
	}

	fg := func(c color.Color, s string) string {
		return lipgloss.NewStyle().Foreground(c).Render(s)
	}

	// Title.
	const spacing = 1
	letterforms := []letterform{
		letterC,
		letterR,
		letterU,
		letterC,
		letterI,
		letterB,
		letterL,
		letterE,
	}
	stretchIndex := -1 // -1 means no stretching.
	if !compact {
		stretchIndex = cachedRandN(len(letterforms))
	}

	crucible := renderWord(spacing, stretchIndex, letterforms...)
	crucibleWidth := lipgloss.Width(crucible)
	b := new(strings.Builder)
	for r := range strings.SplitSeq(crucible, "\n") {
		fmt.Fprintln(b, styles.ApplyForegroundGrad(s, r, o.TitleColorA, o.TitleColorB))
	}
	crucible = b.String()

	// Version row.
	version = ansi.Truncate(version, crucibleWidth, "…")
	gap := max(0, crucibleWidth-lipgloss.Width(version))
	metaRow := strings.Repeat(" ", gap) + fg(o.VersionColor, version)

	// Join the meta row and big Crucible title.
	crucible = strings.TrimSpace(metaRow + "\n" + crucible)

	// Narrow version — wordmark framed with chrome brackets.
	if compact {
		return renderCompact(crucible, crucibleWidth, o)
	}

	fieldHeight := lipgloss.Height(crucible)

	// Left field.
	const leftWidth = 6
	leftField := new(strings.Builder)
	for i := range fieldHeight {
		fmt.Fprintln(leftField, renderFieldRow(leftWidth, i, o.FieldColor, o.StrokeColor))
	}

	// Right field.
	rightWidth := max(15, o.Width-crucibleWidth-leftWidth-2) // 2 for the gap.
	rightField := new(strings.Builder)
	for i := range fieldHeight {
		fmt.Fprint(rightField, renderFieldRow(rightWidth, i, o.FieldColor, o.StrokeColor), "\n")
	}

	// Return the wide version.
	const hGap = " "
	logo := lipgloss.JoinHorizontal(lipgloss.Top, leftField.String(), hGap, crucible, hGap, rightField.String())

	// Corner brackets for FUI framing.
	logo = addCornerBrackets(logo, o.FieldColor)

	if o.Width > 0 {
		// Truncate the logo to the specified width.
		lines := strings.Split(logo, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, o.Width, "")
		}
		logo = strings.Join(lines, "\n")
	}
	return logo
}

// renderCompact renders the narrow wordmark framed with chrome brackets.
func renderCompact(crucible string, crucibleWidth int, o Opts) string {
	w := o.Width
	if w <= 0 {
		w = crucibleWidth
	}

	lines := strings.Split(strings.TrimSpace(crucible), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, w, "")
	}
	crucible = strings.Join(lines, "\n")

	fieldStyle := lipgloss.NewStyle().Foreground(o.FieldColor)
	cornerStyle := lipgloss.NewStyle().Foreground(o.FieldColor)

	topFill := max(0, w-2)
	topBar := cornerStyle.Render("┌") + fieldStyle.Render(strings.Repeat("─", topFill)) + cornerStyle.Render("┐")
	botFill := max(0, w-2)
	botBar := cornerStyle.Render("└") + fieldStyle.Render(strings.Repeat("─", botFill)) + cornerStyle.Render("┘")

	return strings.Join([]string{topBar, crucible, botBar}, "\n")
}

// SmallRender renders a smaller version of the Crucible logo, suitable for
// smaller windows or sidebar usage.
func SmallRender(t *styles.Styles, width int) string {
	title := styles.ApplyBoldForegroundGrad(t, "Crucible", t.Secondary, t.Primary)
	remainingWidth := width - lipgloss.Width(title) - 1 // 1 for the space after "Crucible"
	if remainingWidth > 0 {
		field := lipgloss.NewStyle().
			Foreground(t.BgBase).
			Background(t.Primary).
			Render(strings.Repeat(`╲`, remainingWidth))
		title = fmt.Sprintf("%s %s", title, field)
	}
	return title
}

// renderWord renders letterforms to fork a word. stretchIndex is the index of
// the letter to stretch, or -1 if no letter should be stretched.
func renderWord(spacing int, stretchIndex int, letterforms ...letterform) string {
	if spacing < 0 {
		spacing = 0
	}

	renderedLetterforms := make([]string, len(letterforms))

	// pick one letter randomly to stretch
	for i, letter := range letterforms {
		renderedLetterforms[i] = letter(i == stretchIndex)
	}

	if spacing > 0 {
		// Add spaces between the letters and render.
		renderedLetterforms = slice.Intersperse(renderedLetterforms, strings.Repeat(" ", spacing))
	}
	return strings.TrimSpace(
		lipgloss.JoinHorizontal(lipgloss.Top, renderedLetterforms...),
	)
}

// letterC renders the letter C in a stylized way.
func letterC(stretch bool) string {
	// ▄▀▀▀
	// █
	//  ▀▀▀

	side := heredoc.Doc(`
		▄
		█
	`)
	bars := heredoc.Doc(`
		▀

		▀
	`)
	return joinLetterform(
		side,
		stretchLetterformPart(bars, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
	)
}

// letterR renders the letter R in a stylized way.
func letterR(stretch bool) string {
	// Here's what we're making:
	//
	// █▀▀▀▄
	// █▀▀▀▄
	// ▀   ▀

	left := heredoc.Doc(`
		█
		█
		▀
	`)
	center := heredoc.Doc(`
		▀
		▀
	`)
	right := heredoc.Doc(`
		▄
		▄
		▀
	`)
	return joinLetterform(
		left,
		stretchLetterformPart(center, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
		right,
	)
}

// letterI renders the letter I in a stylized way.
func letterI(stretch bool) string {
	// █
	// █
	// ▀

	_ = stretch // I doesn't stretch well
	return heredoc.Doc(`
		█
		█
		▀`)
}

// letterB renders the letter B in a stylized way.
func letterB(stretch bool) string {
	// █▀▀▀▄
	// █▀▀▀▄
	//  ▀▀▀

	left := heredoc.Doc(`
		█
		█
	`)
	center := heredoc.Doc(`
		▀
		▀
		▀
	`)
	right := heredoc.Doc(`
		▄
		▄
	`)
	return joinLetterform(
		left,
		stretchLetterformPart(center, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
		right,
	)
}

// letterL renders the letter L in a stylized way.
func letterL(stretch bool) string {
	// █
	// █
	// ▀▀▀▀

	left := heredoc.Doc(`
		█
		█
		▀
	`)
	bottom := heredoc.Doc(`


		▀
	`)
	return joinLetterform(
		left,
		stretchLetterformPart(bottom, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
	)
}

// letterE renders the letter E in a stylized way.
func letterE(stretch bool) string {
	// ▄▀▀▀
	// █▀▀▀
	//  ▀▀▀

	left := heredoc.Doc(`
		▄
		█
	`)
	bars := heredoc.Doc(`
		▀
		▀
		▀
	`)
	return joinLetterform(
		left,
		stretchLetterformPart(bars, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
	)
}

// letterU renders the letter U in a stylized way.
func letterU(stretch bool) string {
	// Here's what we're making:
	//
	// █   █
	// █   █
	//	▀▀▀

	side := heredoc.Doc(`
		█
		█
	`)
	middle := heredoc.Doc(`


		▀
	`)
	return joinLetterform(
		side,
		stretchLetterformPart(middle, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
		side,
	)
}

// addCornerBrackets overlays ┌┐└┘ on the first and last non-empty lines.
func addCornerBrackets(logo string, fieldColor color.Color) string {
	cornerStyle := lipgloss.NewStyle().Foreground(fieldColor)
	lines := strings.Split(logo, "\n")
	if len(lines) < 2 {
		return logo
	}
	first := lines[0]
	if w := lipgloss.Width(first); w > 2 {
		lines[0] = cornerStyle.Render("┌") + ansi.Cut(first, 1, w-1) + cornerStyle.Render("┐")
	}
	lastIdx := len(lines) - 1
	for lastIdx > 0 && strings.TrimSpace(lines[lastIdx]) == "" {
		lastIdx--
	}
	last := lines[lastIdx]
	if w := lipgloss.Width(last); w > 2 {
		lines[lastIdx] = cornerStyle.Render("└") + ansi.Cut(last, 1, w-1) + cornerStyle.Render("┘")
	}
	return strings.Join(lines, "\n")
}

// renderFieldRow renders a row of the diagonal stripe field.
// Dark ╲ slashes on a colored background, offset per row for the diagonal effect.
// Even rows are dimmed slightly to simulate CRT scanlines.
func renderFieldRow(width, row int, fieldColor, strokeColor color.Color) string {
	bg := fieldColor
	if row%2 == 0 {
		bg = dimColor(fieldColor, 0.30)
	}
	style := lipgloss.NewStyle().Foreground(strokeColor).Background(bg)
	b := new(strings.Builder)
	for col := range width {
		if (col+row)%fieldStride == 0 {
			b.WriteString(`╲`)
		} else {
			b.WriteByte(' ')
		}
	}
	return style.Render(b.String())
}

// dimColor darkens a color by the given factor (0.0 = no change, 1.0 = black).
func dimColor(c color.Color, factor float64) color.Color {
	r, g, b, a := c.RGBA()
	scale := 1 - factor
	return color.RGBA{
		R: uint8(min(float64(r>>8)*scale, 255)), //nolint:gosec
		G: uint8(min(float64(g>>8)*scale, 255)), //nolint:gosec
		B: uint8(min(float64(b>>8)*scale, 255)), //nolint:gosec
		A: uint8(min(a>>8, 255)),                //nolint:gosec
	}
}

func joinLetterform(letters ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, letters...)
}

// letterformProps defines letterform stretching properties.
// for readability.
type letterformProps struct {
	width      int
	minStretch int
	maxStretch int
	stretch    bool
}

// stretchLetterformPart is a helper function for letter stretching. If randomize
// is false the minimum number will be used.
func stretchLetterformPart(s string, p letterformProps) string {
	if p.maxStretch < p.minStretch {
		p.minStretch, p.maxStretch = p.maxStretch, p.minStretch
	}
	n := p.width
	if p.stretch {
		n = cachedRandN(p.maxStretch-p.minStretch) + p.minStretch //nolint:gosec
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = s
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
