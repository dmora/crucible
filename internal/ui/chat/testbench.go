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

// stationNames holds registered station tool names for UI widget matching.
// Populated at startup via RegisterStationNames from config.DefaultStations keys.
var stationNames = map[string]bool{}

// RegisterStationNames registers station tool names so the UI renders them
// with the StationToolMessageItem widget (live activity, spinner, expand/collapse)
// instead of the generic tool widget.
func RegisterStationNames(names []string) {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	stationNames = m
}

// IsStationTool reports whether a tool name is a registered station.
func IsStationTool(name string) bool {
	return stationNames[name]
}

// maxVisibleActivity is how many activity entries to show in the tail window.
const maxVisibleActivity = 5

// stationParams represents the parameters for a station tool call.
type stationParams struct {
	Task string `json:"task"`
}

// StationToolMessageItem renders a factory station tool with live activity from the sub-agent.
// Used for all station tools (draft, inspect, fabricate, test, etc.).
type StationToolMessageItem struct {
	*baseToolMessageItem
	stationName string // display name for the header (e.g. "Draft", "Inspect")
	activity    []agent.ProcessActivity
	phase       agent.ProcessPhase
	startedAt   time.Time
	endedAt     time.Time // frozen when result/cancel arrives; zero while running
	branch      string    // worktree branch name (empty when not using worktrees)
}

var _ ToolMessageItem = (*StationToolMessageItem)(nil)

// NewStationToolMessageItem creates a new [StationToolMessageItem].
func NewStationToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
	stationName string,
) *StationToolMessageItem {
	t := &StationToolMessageItem{stationName: stationName}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &stationToolRenderContext{st: t}, canceled)
	return t
}

// SetBranch sets the worktree branch name for the station card badge.
func (t *StationToolMessageItem) SetBranch(branch string) {
	t.branch = branch
}

// HasResult reports whether the station tool has received a result.
func (t *StationToolMessageItem) HasResult() bool {
	return t.result != nil
}

// SetActivity updates the activity log, phase, startedAt, and invalidates the render cache.
func (t *StationToolMessageItem) SetActivity(
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
func (t *StationToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == t.ID() {
		return t.anim.Animate(msg)
	}
	return nil
}

// StepOnce advances the spinner one frame without scheduling a tick.
// Used for event-driven animation: the spinner moves with agent activity.
func (t *StationToolMessageItem) StepOnce() {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return
	}
	t.anim.StepOnce()
	t.clearCache()
}

// InvalidateCache clears the render cache so the next draw recomputes
// elapsed time and status chip. Does NOT advance the spinner.
func (t *StationToolMessageItem) InvalidateCache() {
	if t.result != nil || t.Status() == ToolStatusCanceled {
		return
	}
	t.clearCache()
}

// SetResult freezes the elapsed timer and delegates to the base implementation.
func (t *StationToolMessageItem) SetResult(res *message.ToolResult) {
	if res != nil && t.endedAt.IsZero() && !t.startedAt.IsZero() {
		t.endedAt = time.Now()
	}
	t.baseToolMessageItem.SetResult(res)
}

// SetStatus freezes the elapsed timer on cancellation and delegates to the base.
func (t *StationToolMessageItem) SetStatus(status ToolStatus) {
	if status == ToolStatusCanceled && t.endedAt.IsZero() && !t.startedAt.IsZero() {
		t.endedAt = time.Now()
	}
	t.baseToolMessageItem.SetStatus(status)
}

// stationToolRenderContext renders station tool messages.
type stationToolRenderContext struct {
	st *StationToolMessageItem
}

// elapsed returns the formatted elapsed time — frozen if ended, live if running.
func (r *stationToolRenderContext) elapsed() string {
	if r.st.startedAt.IsZero() {
		return ""
	}
	if !r.st.endedAt.IsZero() {
		return common.FormatDuration(r.st.endedAt.Sub(r.st.startedAt))
	}
	return common.FormatElapsed(r.st.startedAt)
}

// RenderTool implements the [ToolRenderer] interface.
func (r *stationToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)

	// Pending: no activity yet, no result, not waiting for permission.
	if !opts.ToolCall.State.IsTerminal() && !opts.IsCanceled() &&
		opts.Status != ToolStatusAwaitingPermission && len(r.st.activity) == 0 {
		return pendingTool(sty, r.st.stationName, opts.Anim)
	}

	summary := ComputeSummary(r.st.activity, r.st.phase, opts.Status, opts.Result)

	var params stationParams
	_ = json.Unmarshal([]byte(opts.ToolCall.Input), &params)

	header := r.stationHeader(sty, summary, cappedWidth)
	if opts.Compact {
		return r.compactLine(sty, header, summary)
	}

	return r.fullRender(sty, opts, summary, header, params.Task, cappedWidth)
}

// stationHeader builds the enriched header: "● Draft · Editing · 0:42"
func (r *stationToolRenderContext) stationHeader(sty *styles.Styles, summary StationSummary, _ int) string {
	icon := stationIcon(sty, summary.State)
	name := sty.Tool.NameNormal.Render(r.st.stationName)
	chip := stateChipStyle(sty, summary.State).Render(string(summary.State))
	sep := sty.Muted.Render(" · ")

	parts := icon + " " + name + sep + chip

	if r.st.branch != "" {
		parts += sep + sty.Subtle.Render("⎇ "+r.st.branch)
	}

	if elapsed := r.elapsed(); elapsed != "" {
		parts += sep + sty.Subtle.Render(elapsed)
	}

	return parts
}

// compactLine renders the compact one-liner with summary counters.
func (r *stationToolRenderContext) compactLine(sty *styles.Styles, header string, summary StationSummary) string {
	sep := sty.Muted.Render(" · ")
	counters := compactCounters(sty, summary)
	if counters != "" {
		return header + sep + counters
	}
	return header
}

// fullRender builds the full station card with task, activity tree, verdict, and result.
func (r *stationToolRenderContext) fullRender(
	sty *styles.Styles,
	opts *ToolRenderOpts,
	summary StationSummary,
	header, task string,
	cappedWidth int,
) string {
	// Build the task tag + prompt.
	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)
	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3)

	taskOneLine := strings.ReplaceAll(task, "\n", " ")
	taskText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(taskOneLine)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, taskTag, " ", taskText),
	)

	// Tree prefix width calculation.
	treeLPadding := 2
	treeDashWidth := max(taskTagWidth-5, 2)
	treePrefixWidth := treeLPadding + 1 + treeDashWidth + 1
	activityWidth := cappedWidth - treePrefixWidth

	running := !opts.HasResult() && !opts.IsCanceled() && opts.Status != ToolStatusAwaitingPermission
	expanded := opts.ExpandedContent

	// Build tree with summarized activity lines.
	childTools := tree.Root(header)
	visibleActivity, hidden := tailActivity(r.st.activity, running, expanded)
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

	// Show spinner with phase if still running, or canceled/permission indicator.
	if running {
		parts = append(parts, "", phaseSpinner(sty, r.st.phase, opts.Anim))
	} else if opts.IsCanceled() && !opts.HasResult() {
		parts = append(parts, "", sty.Tool.StateCancelled.Render("Canceled."))
	} else if opts.Status == ToolStatusAwaitingPermission {
		parts = append(parts, "", sty.Tool.StationChipWarning.Render("⏳ Awaiting approval"))
	}

	// Verdict line for completed stations.
	if opts.HasResult() {
		parts = append(parts, "", verdictLine(sty, summary, cappedWidth))
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Add body content when completed.
	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, expanded)
		return joinToolParts(result, body)
	}

	return result
}

// verdictLine renders the structured completion summary.
func verdictLine(sty *styles.Styles, summary StationSummary, width int) string {
	sep := sty.Muted.Render(" · ")
	var parts []string

	if summary.State == agent.OpStateFailed {
		parts = append(parts, sty.Tool.StationChipError.Render("Failed"))
	} else {
		parts = append(parts, sty.Subtle.Render("Done"))
	}

	if summary.Errors > 0 {
		parts = append(parts, sty.Tool.StationChipError.Render(fmt.Sprintf("%d errors", summary.Errors)))
	}
	if summary.FilesRead > 0 {
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d files read", summary.FilesRead)))
	}
	if summary.FilesEdited > 0 {
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d edited", summary.FilesEdited)))
	}
	if summary.CommandsRun > 0 {
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d commands", summary.CommandsRun)))
	}

	line := sty.Subtle.MaxWidth(width).Render(strings.Join(parts, sep))

	if summary.ResultLine != "" {
		line += "\n" + sty.Subtle.MaxWidth(width).Render("▸ "+summary.ResultLine)
	}

	return line
}

// compactCounters renders the compact counter suffix for the one-liner.
func compactCounters(sty *styles.Styles, summary StationSummary) string {
	sep := sty.Muted.Render(" · ")
	var parts []string

	if summary.Errors > 0 {
		parts = append(parts, sty.Tool.StationChipError.Render(fmt.Sprintf("%d errors", summary.Errors)))
	}
	if summary.FilesRead > 0 {
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d read", summary.FilesRead)))
	}
	if summary.FilesEdited > 0 {
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d edited", summary.FilesEdited)))
	}
	if summary.CommandsRun > 0 {
		label := "cmds"
		if summary.CommandsRun == 1 {
			label = "cmd"
		}
		parts = append(parts, sty.Subtle.Render(fmt.Sprintf("%d %s", summary.CommandsRun, label)))
	}

	return strings.Join(parts, sep)
}

// stationIcon returns the status icon for a station card header.
func stationIcon(sty *styles.Styles, state agent.OperatorState) string {
	switch state {
	case agent.OpStateFailed:
		return sty.Tool.IconError.String()
	case agent.OpStateDone:
		return sty.Tool.IconSuccess.String()
	case agent.OpStateCanceled:
		return sty.Tool.IconCancelled.String()
	default:
		return sty.Tool.IconPending.String()
	}
}

// tailActivity returns the visible slice of activity and how many were hidden.
// When expanded, shows all entries. Otherwise tails to maxVisibleActivity.
func tailActivity(all []agent.ProcessActivity, _, expanded bool) (visible []agent.ProcessActivity, hidden int) {
	if expanded {
		return all, 0
	}
	if len(all) <= maxVisibleActivity {
		return all, 0
	}
	hidden = len(all) - maxVisibleActivity
	return all[hidden:], hidden
}

// phaseSpinner renders the animated spinner with an optional phase label.
func phaseSpinner(sty *styles.Styles, phase agent.ProcessPhase, a anim.SpinnerBackend) string {
	var spinner string
	if a != nil {
		spinner = a.Render()
	}
	if phase != "" {
		return sty.Subtle.Render(string(phase)) + " " + spinner
	}
	return spinner
}
