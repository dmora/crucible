package model

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

const powerlineSep = " ◆ "

// drawPowerline renders the operational info bar: model name, designation, context %, tokens, cost.
// supervisorTokens/contextWindow drive the fill % gauge. totalTokens and cost are factory-wide cumulative.
func drawPowerline(scr uv.Screen, area uv.Rectangle, t *styles.Styles, modelName, designation string, supervisorTokens, contextWindow int64, totalTokens int64, cost float64, holdActive bool, worktreeBranch string) {
	width := area.Dx()
	if width <= 0 {
		return
	}

	sep := t.Status.MetricsMuted.Render(powerlineSep)

	// Left side: model name + designation + hold indicator.
	left := t.Status.Metrics.Render("⏣ "+modelName) + sep + t.Status.MetricsMuted.Render("⚿ "+designation)
	if holdActive {
		left += sep + t.Status.MetricsWarning.Render("HOLD")
	}
	if worktreeBranch != "" {
		left += sep + t.Status.Metrics.Render("⎇ "+worktreeBranch)
	}

	// Right side: context % (supervisor) + tokens (factory-wide) + cost (factory-wide)
	percent := int((float64(supervisorTokens) / float64(contextWindow)) * 100)
	percentStr := fmt.Sprintf("%d%%", percent)
	if percent > 80 {
		percentStr = "W " + t.Status.MetricsWarning.Render(percentStr)
	} else {
		percentStr = t.Status.Metrics.Render(percentStr)
	}
	tokenStr := t.Status.MetricsMuted.Render(fmt.Sprintf("(%s)", common.FormatTokenCount(totalTokens)))
	costStr := t.Status.MetricsMuted.Render(fmt.Sprintf("$%.2f", cost))
	right := percentStr + " " + tokenStr + " " + costStr

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	pad := 2 // left+right padding

	if leftWidth+rightWidth+pad+1 > width {
		// Truncate left side to fit
		left = ansi.Truncate(left, max(0, width-rightWidth-pad-1), "…")
	}

	gap := max(1, width-pad-lipgloss.Width(left)-rightWidth)
	line := left + strings.Repeat(" ", gap) + right

	rendered := lipgloss.NewStyle().Padding(0, 1).Width(width).Render(line)
	uv.NewStyledString(rendered).Draw(scr, area)
}
