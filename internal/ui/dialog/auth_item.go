package dialog

import (
	"charm.land/lipgloss/v2"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// AuthMethodItem is a list item representing an available auth method.
type AuthMethodItem struct {
	avail   config.AuthAvailability
	t       *styles.Styles
	m       fuzzy.Match
	focused bool
	cache   map[int]string
}

var _ ListItem = (*AuthMethodItem)(nil)

func newAuthMethodItem(t *styles.Styles, avail config.AuthAvailability) *AuthMethodItem {
	return &AuthMethodItem{avail: avail, t: t, cache: map[int]string{}}
}

func (a *AuthMethodItem) ID() string        { return string(a.avail.Method) }
func (a *AuthMethodItem) Filter() string    { return a.avail.Label }
func (a *AuthMethodItem) SetFocused(f bool) { a.focused = f }
func (a *AuthMethodItem) SetMatch(m fuzzy.Match) {
	a.m = m
	a.cache = map[int]string{}
}

func (a *AuthMethodItem) Render(width int) string {
	if cached, ok := a.cache[width]; ok {
		return cached
	}

	t := a.t
	label := a.avail.Label
	detail := a.avail.Detail

	var prefix string
	if a.avail.Status == config.AuthStatusCurrent {
		prefix = t.Tool.IconSuccess.Render("✓") + " "
	} else {
		prefix = "  "
	}

	var labelStyle, detailStyle lipgloss.Style
	switch {
	case a.avail.Status == config.AuthStatusUnavailable:
		labelStyle = t.Muted
		detailStyle = t.Muted
	case a.focused:
		labelStyle = t.Dialog.PrimaryText
		detailStyle = t.Subtle
	default:
		labelStyle = t.Subtle
		detailStyle = t.Muted
	}

	rendered := prefix + labelStyle.Render(label)
	if detail != "" {
		rendered += " " + detailStyle.Render(detail)
	}

	a.cache[width] = rendered
	return rendered
}
