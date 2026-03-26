package model

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

// selectedLargeModel returns the currently selected large language model from
// the agent coordinator, if one exists.
func (m *UI) selectedLargeModel() *agent.Model {
	if m.com.App.AgentCoordinator != nil {
		model := m.com.App.AgentCoordinator.Model()
		return &model
	}
	return nil
}

// landingView renders the landing page view showing the current working
// directory, model information, station configuration, and MCP status.
func (m *UI) landingView() string {
	t := m.com.Styles
	width := m.layout.main.Dx()
	cwd := common.PrettyPath(t, m.com.Config().WorkingDir(), width)

	infoSection := lipgloss.JoinVertical(lipgloss.Left, cwd, "", m.modelInfo(width))

	_, remainingHeightArea := layout.SplitVertical(m.layout.main, layout.Fixed(lipgloss.Height(infoSection)+1))

	sectionWidth := min(60, width)
	remainingHeight := max(1, remainingHeightArea.Dy())

	stationsSection := stationConfigInfo(t, m.com.Config().Stations, sectionWidth)
	mcpSection := m.mcpInfo(sectionWidth, max(1, remainingHeight-lipgloss.Height(stationsSection)-1), false)

	return lipgloss.NewStyle().
		Width(width).
		Height(m.layout.main.Dy() - 1).
		PaddingTop(1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, infoSection, "", stationsSection, "", mcpSection),
		)
}

// stationConfigInfo renders the station configuration section for the landing page.
func stationConfigInfo(t *styles.Styles, stations map[string]config.StationConfig, width int) string {
	title := t.ResourceGroupTitle.Render("Stations")

	if len(stations) == 0 {
		list := t.ResourceAdditionalText.Render("None")
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
	}

	names := make([]string, 0, len(stations))
	for name := range stations {
		names = append(names, name)
	}
	slices.Sort(names)

	rows := make([]string, 0, len(names))
	for _, name := range names {
		cfg := stations[name]
		icon := t.ResourceOnlineIcon.String()
		if cfg.Disabled {
			icon = t.ResourceOfflineIcon.String()
		}

		desc := stationDesc(cfg)
		line := common.Status(t, common.StatusOpts{
			Icon:        icon,
			Title:       t.ResourceName.Render(name),
			Description: t.ResourceStatus.Render(desc),
		}, width)

		if constraints := stationConstraints(cfg); constraints != "" {
			indent := "           "
			line += "\n" + t.ResourceAdditionalText.Render(indent+constraints)
		}
		rows = append(rows, line)
	}

	list := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// stationDesc builds a compact description like "claude · plan · feature-dev:feature-dev".
func stationDesc(cfg config.StationConfig) string {
	parts := make([]string, 0, 3) //nolint:mnd
	if cfg.Backend != "" {
		parts = append(parts, cfg.Backend)
	}
	if m := cfg.Options["mode"]; m != "" {
		parts = append(parts, m)
	}
	if cfg.Skill != "" {
		parts = append(parts, cfg.Skill)
	}
	return strings.Join(parts, " · ")
}

// stationConstraints builds a compact route constraint string like
// "requires: plan · after done: review". Returns "" if no constraints.
func stationConstraints(cfg config.StationConfig) string {
	var parts []string
	if len(cfg.Requires) > 0 {
		parts = append(parts, "requires: "+strings.Join(cfg.Requires, ", "))
	}
	if len(cfg.AfterDone) > 0 {
		parts = append(parts, "after done: "+strings.Join(cfg.AfterDone, ", "))
	}
	if cfg.Gate {
		parts = append(parts, "gate")
	}
	return strings.Join(parts, " · ")
}
