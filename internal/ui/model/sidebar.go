package model

import (
	"cmp"
	"fmt"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/fsext"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/logo"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// modelInfo renders the current model information including reasoning
// settings and context usage/cost for the sidebar.
func (m *UI) modelInfo(width int) string {
	model := m.selectedLargeModel()
	reasoningInfo := ""
	providerName := ""

	if model != nil {
		// Get provider name first
		providerConfig, ok := m.com.Config().Providers.Get(model.ModelCfg.Provider)
		if ok {
			providerName = providerConfig.Name

			// Only check reasoning if model can reason
			if model.Metadata.CanReason {
				if len(model.Metadata.ReasoningLevels) == 0 {
					if model.ModelCfg.Think {
						reasoningInfo = "Thinking On"
					} else {
						reasoningInfo = "Thinking Off"
					}
				} else {
					formatter := cases.Title(language.English, cases.NoLower)
					reasoningEffort := cmp.Or(model.ModelCfg.ReasoningEffort, model.Metadata.DefaultReasoningEffort)
					reasoningInfo = formatter.String(fmt.Sprintf("Reasoning %s", reasoningEffort))
				}
			}
		}
	}

	var authInfo *config.AuthInfo
	if model != nil {
		auth := model.Auth
		authInfo = &auth
	}
	return common.ModelInfo(m.com.Styles, model.Metadata.Name, providerName, reasoningInfo, authInfo, width)
}

// activePlanInfo renders the Active Plan section showing the artifact path
// of the most recent plan station dispatch as a clickable link. Only shown
// when the latest plan dispatch completed successfully with an artifact.
func (m *UI) activePlanInfo(width int) string {
	var artifactPath string
	for i := len(m.dispatchLog) - 1; i >= 0; i-- {
		e := m.dispatchLog[i]
		if e.Station == "plan" {
			if e.Verdict == agent.VerdictDone && e.ArtifactPath != "" {
				artifactPath = e.ArtifactPath
			}
			break // always stop at the most recent plan, even if failed/running
		}
	}
	if artifactPath == "" {
		return ""
	}

	t := m.com.Styles
	title := common.Section(t, t.ResourceGroupTitle.Render("Active Plan"), width)
	displayPath := fsext.PrettyPath(artifactPath)
	link := t.Muted.Underline(true).
		Hyperlink("file://" + artifactPath).
		Render(displayPath)

	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, link))
}

// getDynamicHeightLimits allocates sidebar item slots using demand-based sizing
// with station priority. Each section gets a minimum floor, then extras are
// distributed: Stations first, then Files, then MCPs.
func getDynamicHeightLimits(availableHeight, stationDemand, fileDemand, mcpDemand int) (maxFiles, maxStations, maxMCPs int) {
	const (
		minPerSection        = 2
		defaultMaxFilesShown = 10
		defaultMaxMCPsShown  = 8
		totalMinimum         = minPerSection * 3 // 6
	)

	// Tiny terminal: everyone gets the minimum floor.
	if availableHeight < totalMinimum {
		return minPerSection, minPerSection, minPerSection
	}

	// Reserve minimums upfront, then distribute extras.
	budget := availableHeight - totalMinimum

	// Cap demands to configured maximums, compute extra beyond minimum.
	stationExtra := max(0, stationDemand-minPerSection)
	fileExtra := max(0, min(fileDemand, defaultMaxFilesShown)-minPerSection)
	mcpExtra := max(0, min(mcpDemand, defaultMaxMCPsShown)-minPerSection)

	// Priority allocation: Stations → Files → MCPs.
	stationAlloc := min(stationExtra, budget)
	budget -= stationAlloc

	fileAlloc := min(fileExtra, budget)
	budget -= fileAlloc

	mcpAlloc := min(mcpExtra, budget)

	return minPerSection + fileAlloc, minPerSection + stationAlloc, minPerSection + mcpAlloc
}

// drawSidebar renders the chat sidebar containing session title, working
// directory, model info, active plan, file list, and MCP status.
func (m *UI) drawSidebar(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	const (
		logoHeightBreakpoint = 30
		logoWidthBreakpoint  = 40 // block letters need ~40 cells
	)

	t := m.com.Styles
	width, height := area.Dx(), area.Dy()

	title := t.Muted.Width(width).MaxHeight(2).Render(m.session.Title)
	cwd := common.PrettyPath(t, m.com.Config().WorkingDir(), width)
	sidebarLogo := m.sidebarLogo
	if height < logoHeightBreakpoint || width < logoWidthBreakpoint {
		sidebarLogo = logo.SmallRender(m.com.Styles, width)
	}
	blocks := []string{
		sidebarLogo,
		title,
		"",
		cwd,
	}
	// Show worktree branch indicator if active.
	if m.session != nil && m.com.App.AgentCoordinator != nil {
		if wt := m.com.App.AgentCoordinator.WorktreeInfo(m.session.ID); wt != nil {
			blocks = append(blocks, t.Subtle.Render("⎇ "+wt.Branch))
		}
	}
	blocks = append(blocks,
		"",
		m.modelInfo(width),
		"",
	)

	sidebarHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	// Active plan section (fixed height, before dynamic sections).
	planSection := m.activePlanInfo(width)
	headerHeight := lipgloss.Height(sidebarHeader)
	if planSection != "" {
		headerHeight += lipgloss.Height(planSection)
	}
	_, remainingHeightArea := layout.SplitVertical(m.layout.sidebar, layout.Fixed(headerHeight))
	const sectionOverhead = 8 // 3 section headers (2 lines each) + 2 inter-section blanks
	remainingHeight := remainingHeightArea.Dy() - sectionOverhead

	stationDemand := stationEntryCount(m.dispatchLog, m.com.Config().Stations)
	filesWithChanges := getFilesWithChanges(m.sessionFiles)
	fileDemand := len(filesWithChanges)
	mcpDemand := len(m.mcpStates)
	maxFiles, maxStations, maxMCPs := getDynamicHeightLimits(remainingHeight, stationDemand, fileDemand, mcpDemand)

	filesSection := m.filesInfo(m.com.Config().WorkingDir(), filesWithChanges, width, maxFiles, true)
	stationsSection := m.processInfo(width, maxStations, true)
	mcpSection := m.mcpInfo(width, maxMCPs, true)

	// Build final content, including plan section if present.
	content := []string{sidebarHeader}
	if planSection != "" {
		content = append(content, planSection)
	}
	content = append(content,
		filesSection,
		"",
		stationsSection,
		"",
		mcpSection,
	)

	uv.NewStyledString(
		lipgloss.NewStyle().
			MaxWidth(width).
			MaxHeight(height).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					content...,
				),
			),
	).Draw(scr, area)
}
