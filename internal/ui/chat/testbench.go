package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	Task            string   `json:"task"`
	TaskDescription string   `json:"task_description,omitempty"`
	Assumptions     []string `json:"assumptions,omitempty"` // keep for replay compat
	ContextHints    []string `json:"context_hints,omitempty"`
	Constraints     []string `json:"constraints,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
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

// StationName implements LiveStationItem.
func (t *StationToolMessageItem) StationName() string {
	return t.ToolCall().Name
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

	return r.fullRender(sty, opts, summary, header, params, cappedWidth)
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

// indentText prepends n spaces to each line of s.
func indentText(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// renderTaskDescription renders the task_description as wrapped prose, indented
// to align with the task prompt text. Returns empty string when desc is empty.
func renderTaskDescription(sty *styles.Styles, desc string, taskTagWidth, cappedWidth int) string {
	if desc == "" {
		return ""
	}
	// Derive indent from the task tag geometry — the description aligns
	// with the task prompt text, not a hardcoded offset.
	descIndent := taskTagWidth + 1 // +1 for the space between tag and prompt
	descWidth := min(cappedWidth-descIndent, maxTextWidth-descIndent)
	if descWidth < 20 {
		// Narrow fallback: drop indent, render full-width below the tag line.
		descIndent = 0
		descWidth = min(cappedWidth, maxTextWidth)
	}
	descText := sty.Subtle.Width(descWidth).Render(desc)
	if descIndent > 0 {
		descText = indentText(descText, descIndent)
	}
	return descText
}

// renderDispatchSections renders the optional structured dispatch fields
// (assumptions, context, constraints, success criteria) beneath the task line.
// Returns empty string when no structured fields are present.
// dispatchSection pairs a heading with its items for structured dispatch rendering.
type dispatchSection struct {
	heading string
	items   []string
}

// filterItems returns non-blank items with whitespace trimmed.
func filterItems(items []string) []string {
	var out []string
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// renderDispatchSection renders a single section tag with its bullet items.
// All items must be pre-filtered (non-blank, trimmed).
func renderDispatchSection(sty *styles.Styles, sec dispatchSection, tagWidth, width int) string {
	tag := sty.Tool.DispatchSectionTag.Render(sec.heading)
	itemWidth := max(min(width-tagWidth-1, maxTextWidth-tagWidth-1), 1)

	lines := make([]string, 0, 1+len(sec.items))
	lines = append(lines, tag)
	for _, item := range sec.items {
		text := sty.Subtle.Width(itemWidth).Render("· " + item)
		// Indent every line of the rendered text (hanging indent for wraps).
		lines = append(lines, indentText(text, tagWidth+1))
	}
	return strings.Join(lines, "\n")
}

func renderDispatchSections(sty *styles.Styles, params stationParams, width int) string {
	// Pre-filter items: trim whitespace and drop blanks before any layout work.
	sections := []dispatchSection{
		{"Assumptions", filterItems(params.Assumptions)},
		{"Context", filterItems(params.ContextHints)},
		{"Constraints", filterItems(params.Constraints)},
		{"Success Criteria", filterItems(params.SuccessCriteria)},
	}

	// Pre-compute max tag width across non-empty sections.
	var maxTagWidth int
	for _, sec := range sections {
		if len(sec.items) == 0 {
			continue
		}
		w := lipgloss.Width(sty.Tool.DispatchSectionTag.Render(sec.heading))
		if w > maxTagWidth {
			maxTagWidth = w
		}
	}

	// Narrow-width fallback: if the terminal is too narrow for widest-tag
	// alignment (itemWidth would be <= 0), fall back to per-tag indentation.
	usePerTagIndent := width <= maxTagWidth+1

	var parts []string
	for _, sec := range sections {
		if len(sec.items) == 0 {
			continue
		}
		tagWidth := maxTagWidth
		if usePerTagIndent {
			tagWidth = lipgloss.Width(sty.Tool.DispatchSectionTag.Render(sec.heading))
		}
		parts = append(parts, renderDispatchSection(sty, sec, tagWidth, width))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// fullRender builds the full station card with task, activity tree, verdict, and result.
func (r *stationToolRenderContext) fullRender(
	sty *styles.Styles,
	opts *ToolRenderOpts,
	summary StationSummary,
	header string,
	params stationParams,
	cappedWidth int,
) string {
	// Build the task tag + prompt.
	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)
	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3)

	taskOneLine := strings.ReplaceAll(params.Task, "\n", " ")
	taskText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(taskOneLine)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, taskTag, " ", taskText),
	)

	// Render task_description as wrapped prose beneath the Task tag line.
	if descText := renderTaskDescription(sty, params.TaskDescription, taskTagWidth, cappedWidth); descText != "" {
		header = lipgloss.JoinVertical(lipgloss.Left, header, descText)
	}

	// Append structured dispatch sections (assumptions, context, constraints, success criteria).
	if sections := renderDispatchSections(sty, params, cappedWidth); sections != "" {
		header = lipgloss.JoinVertical(lipgloss.Left, header, sections)
	}

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
	if summary.ArtifactPath != "" {
		short := filepath.Base(summary.ArtifactPath)
		parts = append(parts, sty.Subtle.Render("spec: "+short))
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
