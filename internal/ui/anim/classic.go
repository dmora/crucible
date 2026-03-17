package anim

import (
	"image/color"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ClassicSpinner cycles through pre-defined frame strings from a bubbles
// spinner preset, driven by the same StepMsg tick used by the industrial Anim.
type ClassicSpinner struct {
	frames     []string
	fps        time.Duration
	frame      atomic.Int64
	style      lipgloss.Style
	labelStyle lipgloss.Style
	id         string
	label      string
	width      int
}

// Compile-time check: *ClassicSpinner satisfies SpinnerBackend.
var _ SpinnerBackend = (*ClassicSpinner)(nil)

// newClassicSpinner creates a ClassicSpinner from a bubbles spinner preset.
func newClassicSpinner(id string, sp spinner.Spinner, primary color.Color, labelColor color.Color, label string) *ClassicSpinner {
	style := lipgloss.NewStyle().Foreground(primary)
	var lblStyle lipgloss.Style
	if labelColor != nil {
		lblStyle = lipgloss.NewStyle().Foreground(labelColor)
	} else {
		lblStyle = lipgloss.NewStyle().Foreground(primary)
	}
	w := 0
	for _, f := range sp.Frames {
		if fw := lipgloss.Width(f); fw > w {
			w = fw
		}
	}
	return &ClassicSpinner{
		frames:     sp.Frames,
		fps:        sp.FPS,
		style:      style,
		labelStyle: lblStyle,
		id:         id,
		label:      label,
		width:      w,
	}
}

// Start begins the animation tick loop.
func (c *ClassicSpinner) Start() tea.Cmd {
	return tea.Tick(c.fps, func(_ time.Time) tea.Msg {
		return StepMsg{ID: c.id}
	})
}

// Animate advances to the next frame and schedules the next tick.
func (c *ClassicSpinner) Animate(msg StepMsg) tea.Cmd {
	if msg.ID != c.id {
		return nil
	}
	f := c.frame.Add(1)
	if int(f) >= len(c.frames) {
		c.frame.Store(0)
	}
	return tea.Tick(c.fps, func(_ time.Time) tea.Msg {
		return StepMsg{ID: c.id}
	})
}

// StepOnce advances one frame without scheduling a next tick.
func (c *ClassicSpinner) StepOnce() {
	f := c.frame.Add(1)
	if int(f) >= len(c.frames) {
		c.frame.Store(0)
	}
}

// Render returns the styled current frame, followed by the label if set.
func (c *ClassicSpinner) Render() string {
	f := int(c.frame.Load()) % len(c.frames)
	s := c.style.Render(c.frames[f])
	if c.label != "" {
		s += labelGap + c.labelStyle.Render(c.label)
	}
	return s
}

// SetLabel updates the label text displayed after the spinner frames.
func (c *ClassicSpinner) SetLabel(label string) {
	c.label = label
}

// FollowsText returns false — classic spinners render at a fixed position.
func (c *ClassicSpinner) FollowsText() bool { return false }

// Width returns the total width in cells (frames + gap + label).
func (c *ClassicSpinner) Width() int {
	w := c.width
	if c.label != "" {
		w += labelGapWidth + lipgloss.Width(c.label)
	}
	return w
}
