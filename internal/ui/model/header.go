package model

import (
	"image/color"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/fsext"
	"github.com/dmora/crucible/internal/session"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

const (
	headerDiag           = "╲"
	minHeaderDiags       = 3
	leftPadding          = 1
	rightPadding         = 1
	diagToDetailsSpacing = 1 // space between diagonal pattern and details section

	glitchDuration = 800 * time.Millisecond
	glitchFPS      = 20
)

// glitchTickMsg is sent on each animation frame during the startup glitch.
type glitchTickMsg struct{}

type header struct {
	// cached logo and compact logo
	logo        string
	compactLogo string

	com     *common.Common
	width   int
	compact bool

	// Startup glitch animation state.
	glitchActive bool
	glitchStart  time.Time
}

// newHeader creates a new header model.
func newHeader(com *common.Common) *header {
	h := &header{
		com:          com,
		glitchActive: true,
	}
	t := com.Styles
	bgSpace := lipgloss.NewStyle().Background(t.Primary).Render(" ")
	h.compactLogo = styles.ApplyBoldForegroundGradWithBg(t, "CRUCIBLE", t.BgBase, t.BgOverlay, t.Primary) + bgSpace
	return h
}

// glitchTick returns a tea.Cmd that schedules the next glitch animation frame.
func glitchTick() tea.Cmd {
	return tea.Tick(time.Second/glitchFPS, func(time.Time) tea.Msg {
		return glitchTickMsg{}
	})
}

// turnMetricsTickMsg is sent every second to update elapsed time in the placeholder.
type turnMetricsTickMsg struct{}

func turnMetricsTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return turnMetricsTickMsg{}
	})
}

// InvalidateCache clears cached logo renders for both compact and wide modes,
// forcing re-render on the next draw with current styles.
func (h *header) InvalidateCache() {
	h.logo = ""
	h.compactLogo = ""
	h.width = 0
	h.compact = false
}

// drawHeader draws the header for the given session.
func (h *header) drawHeader(
	scr uv.Screen,
	area uv.Rectangle,
	session *session.Session,
	compact bool,
	width int,
) {
	t := h.com.Styles
	if width != h.width || compact != h.compact || h.logo == "" {
		h.logo = renderLogo(h.com.Styles, compact, width)
	}
	if h.compactLogo == "" {
		bgSpace := lipgloss.NewStyle().Background(t.Primary).Render(" ")
		h.compactLogo = styles.ApplyBoldForegroundGradWithBg(t, "CRUCIBLE", t.BgBase, t.BgOverlay, t.Primary) + bgSpace
	}

	h.width = width
	h.compact = compact

	if !compact || session == nil || h.com.App == nil {
		logoView := h.logo
		if h.glitchActive && !compact {
			if h.glitchStart.IsZero() {
				h.glitchStart = time.Now()
			}
			elapsed := time.Since(h.glitchStart)
			if elapsed >= glitchDuration {
				h.glitchActive = false
			} else {
				logoView = renderGlitchedLogo(logoView, elapsed, t.LogoFieldColor)
			}
		}
		if !compact && h.com.App != nil && h.com.App.AgentCoordinator != nil {
			if name := h.com.App.AgentCoordinator.AgentName(); name != "" {
				label := lipgloss.NewStyle().Foreground(t.Secondary).Render(strings.ToUpper(name))
				logoView += "\n" + label
			}
		}
		uv.NewStyledString(logoView).Draw(scr, area)
		return
	}

	if session.ID == "" {
		return
	}

	var b strings.Builder
	b.WriteString(h.compactLogo)
	if h.com.App.AgentCoordinator != nil {
		if name := h.com.App.AgentCoordinator.AgentName(); name != "" {
			onBg := lipgloss.NewStyle().Background(t.Primary)
			sep := onBg.Foreground(t.FgMuted).Render("│")
			agentLabel := onBg.Foreground(t.FgBase).Bold(true).Render(strings.ToUpper(name))
			sp := onBg.Render(" ")
			b.WriteString(sep + sp + agentLabel + sp)
		}
	}

	availDetailWidth := width - leftPadding - rightPadding - lipgloss.Width(b.String()) - minHeaderDiags - diagToDetailsSpacing
	details := renderHeaderDetails(
		h.com,
		availDetailWidth,
	)

	remainingWidth := width -
		lipgloss.Width(b.String()) -
		lipgloss.Width(details) -
		leftPadding -
		rightPadding -
		diagToDetailsSpacing

	if remainingWidth > 0 {
		diagCount := max(minHeaderDiags, remainingWidth)
		b.WriteString(t.Header.Diagonals.Render(strings.Repeat(headerDiag, diagCount)))
		b.WriteString(lipgloss.NewStyle().Background(t.Primary).Render(" "))
	}

	b.WriteString(details)

	view := uv.NewStyledString(
		t.Base.Padding(0, rightPadding, 0, leftPadding).Background(t.Primary).Render(b.String()))
	view.Draw(scr, area)
}

// renderGlitchedLogo replaces lines of the resolved logo with random glitch
// characters, resolving top-to-bottom as the animation progresses.
func renderGlitchedLogo(resolved string, elapsed time.Duration, fieldColor color.Color) string {
	progress := float64(elapsed) / float64(glitchDuration)
	lines := strings.Split(resolved, "\n")
	glitchRunes := []rune("0123456789ABCDEF╱╲█▄▀░▒▓")
	glitchStyle := lipgloss.NewStyle().Foreground(fieldColor)
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w == 0 {
			continue
		}
		lineThreshold := float64(i) / float64(max(1, len(lines)))
		if progress < lineThreshold+0.3 {
			var gb strings.Builder
			for range w {
				gb.WriteRune(glitchRunes[rand.IntN(len(glitchRunes))]) //nolint:gosec
			}
			lines[i] = glitchStyle.Render(gb.String())
		}
	}
	return strings.Join(lines, "\n")
}

// renderHeaderDetails renders the details section of the header.
func renderHeaderDetails(
	com *common.Common,
	availWidth int,
) string {
	t := com.Styles
	dot := t.Header.Separator.Render(" ◆ ")

	// Auth user/project (model and designation are in the powerline).
	var parts []string

	var auth config.AuthInfo
	if com.App != nil && com.App.AgentCoordinator != nil {
		auth = com.App.AgentCoordinator.Model().Auth
	}
	if auth.User != "" {
		parts = append(parts, t.Header.WorkingDir.Render(auth.User))
	}
	if auth.Project != "" {
		parts = append(parts, t.Header.WorkingDir.Render(auth.Project))
	}

	metadata := strings.Join(parts, dot)
	metadata = dot + metadata

	const dirTrimLimit = 4
	cfg := com.Config()
	cwd := fsext.DirTrim(fsext.PrettyPath(cfg.WorkingDir()), dirTrimLimit)
	cwd = t.Header.WorkingDir.Render(cwd)

	result := cwd + metadata
	return ansi.Truncate(result, max(0, availWidth), "…")
}
