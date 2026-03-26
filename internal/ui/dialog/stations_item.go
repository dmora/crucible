package dialog

import (
	"cmp"
	"strings"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// StationItem wraps a station config for list rendering.
type StationItem struct {
	name      string
	cfg       config.StationConfig
	isBuiltin bool
	appCfg    *config.Config // for scope detection
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var _ ListItem = (*StationItem)(nil)

func newStationItem(t *styles.Styles, name string, cfg config.StationConfig, isBuiltin bool, appCfg *config.Config) *StationItem {
	return &StationItem{name: name, cfg: cfg, isBuiltin: isBuiltin, appCfg: appCfg, t: t, cache: map[int]string{}}
}

func (s *StationItem) ID() string        { return s.name }
func (s *StationItem) Filter() string    { return s.name }
func (s *StationItem) SetFocused(f bool) { s.focused = f; s.cache = map[int]string{} }
func (s *StationItem) SetMatch(m fuzzy.Match) {
	s.m = m
	s.cache = map[int]string{}
}

func (s *StationItem) Render(width int) string {
	if cached, ok := s.cache[width]; ok {
		return cached
	}

	t := s.t
	nameStyle := t.Subtle
	detailStyle := t.Muted
	if s.focused {
		nameStyle = t.Dialog.PrimaryText
		detailStyle = t.Subtle
	}
	if s.cfg.Disabled {
		nameStyle = t.Muted
		detailStyle = t.Muted
	}

	label := nameStyle.Render(strings.ToUpper(s.name))

	var parts []string
	parts = append(parts, cmp.Or(s.cfg.Backend, "claude"))
	if s.cfg.Skill != "" {
		parts = append(parts, s.cfg.Skill)
	}
	if s.cfg.Gate {
		parts = append(parts, "GATED")
	}
	if s.cfg.Disabled {
		parts = append(parts, "DISABLED")
	}

	// Scope badge.
	if s.appCfg != nil {
		switch s.appCfg.StationScope(s.name) {
		case config.ConfigScopeProject:
			parts = append(parts, "PROJECT")
		case config.ConfigScopeGlobal:
			parts = append(parts, "GLOBAL")
		case config.ConfigScopeUser:
			parts = append(parts, "USER")
		}
	}

	detail := detailStyle.Render(strings.Join(parts, " · "))

	rendered := label + "  " + detail
	s.cache[width] = rendered
	return rendered
}
