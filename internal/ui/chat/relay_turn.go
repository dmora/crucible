package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/anim"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

// LiveStationItem is implemented by chat items that receive live activity updates.
type LiveStationItem interface {
	StationName() string
	SetActivity([]agent.ProcessActivity, agent.ProcessPhase, time.Time)
	HasResult() bool
	Status() ToolStatus
}

// Verify interface conformance at compile time.
var (
	_ LiveStationItem = (*StationToolMessageItem)(nil)
	_ LiveStationItem = (*RelayTurnMessageItem)(nil)
)

// RelayTurnMessageItem renders a direct operator-to-station relay turn.
// Unlike StationToolMessageItem, it has no supervisor tool call — the operator drives directly.
type RelayTurnMessageItem struct {
	*baseToolMessageItem
	stationName string
	operatorMsg string
	activity    []agent.ProcessActivity
	phase       agent.ProcessPhase
	startedAt   time.Time
	endedAt     time.Time
}

var _ ToolMessageItem = (*RelayTurnMessageItem)(nil)

// NewRelayTurnMessageItem creates a new RelayTurnMessageItem.
func NewRelayTurnMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
	stationName string,
) *RelayTurnMessageItem {
	var operatorMsg string
	var params map[string]string
	if json.Unmarshal([]byte(toolCall.Input), &params) == nil {
		operatorMsg = params["message"]
	}

	t := &RelayTurnMessageItem{stationName: stationName, operatorMsg: operatorMsg}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &relayTurnRenderContext{rt: t}, canceled)
	return t
}

// StationName implements LiveStationItem.
func (t *RelayTurnMessageItem) StationName() string {
	return strings.ToLower(t.stationName)
}

// HasResult implements LiveStationItem.
func (t *RelayTurnMessageItem) HasResult() bool {
	return t.result != nil
}

// SetActivity implements LiveStationItem.
func (t *RelayTurnMessageItem) SetActivity(
	activity []agent.ProcessActivity,
	phase agent.ProcessPhase,
	startedAt time.Time,
) {
	t.activity = activity
	t.phase = phase
	t.startedAt = startedAt
	t.clearCache()
}

// Animate progresses the animation.
func (t *RelayTurnMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == t.ID() {
		return t.anim.Animate(msg)
	}
	return nil
}

// StepOnce advances the spinner one frame.
func (t *RelayTurnMessageItem) StepOnce() {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return
	}
	t.anim.StepOnce()
	t.clearCache()
}

// InvalidateCache clears the render cache.
func (t *RelayTurnMessageItem) InvalidateCache() {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return
	}
	t.clearCache()
}

// SetResult freezes the elapsed timer and delegates to the base implementation.
func (t *RelayTurnMessageItem) SetResult(res *message.ToolResult) {
	if res != nil && t.endedAt.IsZero() && !t.startedAt.IsZero() {
		t.endedAt = time.Now()
	}
	t.baseToolMessageItem.SetResult(res)
}

// SetStatus freezes the elapsed timer on cancellation.
func (t *RelayTurnMessageItem) SetStatus(status ToolStatus) {
	if status == ToolStatusCanceled && t.endedAt.IsZero() && !t.startedAt.IsZero() {
		t.endedAt = time.Now()
	}
	t.baseToolMessageItem.SetStatus(status)
}

// relayTurnRenderContext renders relay turn messages.
type relayTurnRenderContext struct {
	rt *RelayTurnMessageItem
}

func (r *relayTurnRenderContext) elapsed() string {
	if r.rt.startedAt.IsZero() {
		return ""
	}
	if !r.rt.endedAt.IsZero() {
		return common.FormatDuration(r.rt.endedAt.Sub(r.rt.startedAt))
	}
	return common.FormatElapsed(r.rt.startedAt)
}

// RenderTool implements the ToolRenderer interface.
func (r *relayTurnRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)

	// Pending: no activity yet.
	if !opts.ToolCall.State.IsTerminal() && !opts.IsCanceled() &&
		opts.Status != ToolStatusAwaitingPermission && len(r.rt.activity) == 0 {
		return pendingTool(sty, r.rt.stationName, opts.Anim)
	}

	summary := ComputeSummary(r.rt.activity, r.rt.phase, opts.Status, opts.Result)

	header := r.relayHeader(sty, summary)
	if opts.Compact {
		return r.compactLine(sty, header, summary)
	}

	return r.fullRender(sty, opts, summary, header, cappedWidth)
}

// relayHeader builds: "● Build · DIRECT · 2m31s"
func (r *relayTurnRenderContext) relayHeader(sty *styles.Styles, summary StationSummary) string {
	icon := stationIcon(sty, summary.State)
	name := sty.Tool.NameNormal.Render(r.rt.stationName)
	directBadge := sty.Tool.StationChipWarning.Render("DIRECT")
	sep := sty.Muted.Render(" · ")

	parts := icon + " " + name + sep + directBadge

	if elapsed := r.elapsed(); elapsed != "" {
		parts += sep + sty.Subtle.Render(elapsed)
	}

	return parts
}

func (r *relayTurnRenderContext) compactLine(sty *styles.Styles, header string, summary StationSummary) string {
	sep := sty.Muted.Render(" · ")
	counters := compactCounters(sty, summary)
	if counters != "" {
		return header + sep + counters
	}
	return header
}

func (r *relayTurnRenderContext) fullRender(
	sty *styles.Styles,
	opts *ToolRenderOpts,
	summary StationSummary,
	header string,
	cappedWidth int,
) string {
	// Build the task line from operator message.
	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)
	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3)

	taskOneLine := strings.ReplaceAll(r.rt.operatorMsg, "\n", " ")
	taskText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(taskOneLine)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, taskTag, " ", taskText),
	)

	treeLPadding := 2
	treeDashWidth := max(taskTagWidth-5, 2)
	treePrefixWidth := treeLPadding + 1 + treeDashWidth + 1
	activityWidth := cappedWidth - treePrefixWidth

	running := !opts.HasResult() && !opts.IsCanceled() && opts.Status != ToolStatusAwaitingPermission
	expanded := opts.ExpandedContent

	childTools := tree.Root(header)
	visibleActivity, hidden := tailActivity(r.rt.activity, running, expanded)
	summarized := summarizeActivity(visibleActivity)

	if hidden > 0 {
		hint := sty.Tool.ContentTruncation.Render(
			fmt.Sprintf("… %d more [click to expand]", hidden),
		)
		childTools.Child(hint)
	}
	for _, line := range summarized {
		childTools.Child(sty.Subtle.Width(activityWidth).Render(line))
	}

	var parts []string
	parts = append(parts, childTools.
		Enumerator(roundedEnumerator(treeLPadding, treeDashWidth)).
		Indenter(roundedIndenter(treeLPadding, treeDashWidth)).
		String())

	if running {
		parts = append(parts, "", phaseSpinner(sty, r.rt.phase, opts.Anim))
	} else if opts.IsCanceled() && !opts.HasResult() {
		parts = append(parts, "", sty.Tool.StateCancelled.Render("Canceled."))
	}

	if opts.HasResult() {
		parts = append(parts, "", verdictLine(sty, summary, cappedWidth))
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, expanded)
		return joinToolParts(result, body)
	}

	return result
}
