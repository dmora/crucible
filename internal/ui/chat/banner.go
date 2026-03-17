package chat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dmora/crucible/internal/agent/tools/mcp"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/styles"
)

// BannerStation holds display data for a single station row.
type BannerStation struct {
	Name    string
	Backend string
	Mode    string // "plan", "act", or ""
	Skill   string // short name after ":"
	Gate    bool
}

// BannerMCP holds display data for a single MCP server row.
type BannerMCP struct {
	Name  string
	Tools int
}

// BannerConfig holds all data needed to render the factory status banner.
type BannerConfig struct {
	Stations   []BannerStation
	MCPServers []BannerMCP
	Supervisor string
}

// BannerMessageItem renders the factory status as a box-drawn FUI panel.
type BannerMessageItem struct {
	*cachedMessageItem

	id  string
	sty *styles.Styles
	cfg BannerConfig
}

var _ MessageItem = (*BannerMessageItem)(nil)

// NewBannerMessageItem creates a new factory status banner.
func NewBannerMessageItem(sty *styles.Styles, cfg BannerConfig) MessageItem {
	return &BannerMessageItem{
		cachedMessageItem: &cachedMessageItem{},
		id:                fmt.Sprintf("factory-status-banner-%d", time.Now().UnixNano()),
		sty:               sty,
		cfg:               cfg,
	}
}

// ID implements MessageItem.
func (b *BannerMessageItem) ID() string { return b.id }

// RawRender implements MessageItem.
func (b *BannerMessageItem) RawRender(width int) string {
	innerWidth := max(0, width-MessageLeftPaddingTotal)
	content, _, ok := b.getCachedRender(innerWidth)
	if !ok {
		content = b.renderContent(innerWidth)
		b.setCachedRender(content, innerWidth, lipgloss.Height(content))
	}
	return content
}

// Render implements MessageItem.
func (b *BannerMessageItem) Render(width int) string {
	prefix := b.sty.Chat.Message.SectionHeader.Render()
	lines := strings.Split(b.RawRender(width), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func (b *BannerMessageItem) renderContent(width int) string {
	// Box border consumes 2 chars on each side + 1 padding each side = 6 total.
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.sty.Primary).
		Padding(0, 1).
		Width(max(0, width-2)) // account for border width

	var sections []string

	// Stations section.
	if len(b.cfg.Stations) > 0 {
		sections = append(sections, b.renderStations())
	}

	// MCP section.
	if len(b.cfg.MCPServers) > 0 {
		sections = append(sections, b.renderMCPs())
	}

	// Supervisor section.
	if b.cfg.Supervisor != "" {
		header := b.sty.ResourceGroupTitle.Render("SUPERVISOR")
		model := b.sty.Subtle.Render(b.cfg.Supervisor)
		sections = append(sections, header+"\n"+model)
	}

	title := lipgloss.NewStyle().
		Foreground(b.sty.Primary).
		Bold(true).
		Render("FACTORY STATUS")

	body := strings.Join(sections, "\n\n")
	inner := title + "\n\n" + body

	return boxStyle.Render(inner)
}

func (b *BannerMessageItem) renderStations() string {
	header := b.sty.ResourceGroupTitle.Render("STATIONS")

	rows := make([]string, 0, len(b.cfg.Stations))
	for _, s := range b.cfg.Stations {
		parts := []string{
			b.sty.ResourceOnlineIcon.String(),
			b.sty.ResourceName.Render(s.Name),
			b.sty.Subtle.Render(s.Backend),
		}
		if s.Mode != "" {
			parts = append(parts, b.sty.HalfMuted.Render(s.Mode))
		}
		if s.Skill != "" {
			parts = append(parts, b.sty.Muted.Render(s.Skill))
		}
		if s.Gate {
			parts = append(parts, b.sty.ResourceStatus.Render("gate"))
		}
		rows = append(rows, strings.Join(parts, "  "))
	}

	return header + "\n" + strings.Join(rows, "\n")
}

func (b *BannerMessageItem) renderMCPs() string {
	header := b.sty.ResourceGroupTitle.Render("MCP SERVERS")

	rows := make([]string, 0, len(b.cfg.MCPServers))
	for _, m := range b.cfg.MCPServers {
		icon := b.sty.ResourceOnlineIcon.String()
		name := b.sty.ResourceName.Render(m.Name)
		tools := b.sty.Subtle.Render(fmt.Sprintf("%d tools", m.Tools))
		rows = append(rows, fmt.Sprintf("%s  %s  %s", icon, name, tools))
	}

	return header + "\n" + strings.Join(rows, "\n")
}

// BuildBannerConfig extracts display data from config and MCP runtime state.
func BuildBannerConfig(cfg *config.Config, mcpStates map[string]mcp.ClientInfo) BannerConfig {
	bc := BannerConfig{
		Stations:   buildBannerStations(cfg.Stations),
		MCPServers: buildBannerMCPs(cfg, mcpStates),
	}

	// Supervisor model.
	if agentCfg, ok := cfg.Agents[config.AgentCrucible]; ok {
		model := cfg.GetModelByType(agentCfg.Model)
		if model != nil {
			bc.Supervisor = model.Name
		} else {
			bc.Supervisor = string(agentCfg.Model)
		}
	}

	return bc
}

func buildBannerStations(stations map[string]config.StationConfig) []BannerStation {
	names := make([]string, 0, len(stations))
	for name := range stations {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]BannerStation, 0, len(names))
	for _, name := range names {
		sc := stations[name]
		if sc.Disabled {
			continue
		}
		mode := sc.Options["mode"]
		skill := sc.Skill
		if idx := strings.LastIndex(skill, ":"); idx >= 0 && idx < len(skill)-1 {
			skill = skill[idx+1:]
		}
		backend := sc.Backend
		if backend == "" {
			backend = "claude"
		}
		result = append(result, BannerStation{
			Name:    name,
			Backend: backend,
			Mode:    mode,
			Skill:   skill,
			Gate:    sc.Gate,
		})
	}
	return result
}

func buildBannerMCPs(cfg *config.Config, mcpStates map[string]mcp.ClientInfo) []BannerMCP {
	var result []BannerMCP
	for _, mcpCfg := range cfg.MCP.Sorted() {
		if state, ok := mcpStates[mcpCfg.Name]; ok && state.State == mcp.StateConnected {
			result = append(result, BannerMCP{
				Name:  mcpCfg.Name,
				Tools: state.Counts.Tools,
			})
		}
	}
	return result
}
