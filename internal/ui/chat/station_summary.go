package chat

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/styles"
)

// Tool name constants used across classification and counting.
const (
	toolView        = "view"
	toolRead        = "read"
	toolWrite       = "write"
	toolEdit        = "edit"
	toolMultiedit   = "multiedit"
	toolBash        = "bash"
	toolGlob        = "glob"
	toolGrep        = "grep"
	toolLS          = "ls"
	toolWebSearch   = "web_search"
	toolWebFetch    = "web_fetch"
	toolFetch       = "fetch"
	toolAgenticF    = "agentic_fetch"
	toolDownload    = "download"
	toolSourcegraph = "sourcegraph"
	toolTodos       = "todos"
	toolAgent       = "agent"
	toolJobOutput   = "job_output"
)

// StationSummary holds derived operator-facing metrics. Computed at render
// time from live activity, phase, ToolStatus, and ToolResult — never stored
// as a field, so it is always consistent with the card's current state.
type StationSummary struct {
	State       agent.OperatorState
	FilesRead   int
	FilesEdited int
	CommandsRun int
	TestsRun    int
	Errors      int
	LastAction  string
	ResultLine  string
}

// DeriveOperatorState derives the operator-facing state from raw process data.
// Priority: ToolStatus overrides > phase > last activity > fallback.
func DeriveOperatorState(
	activity []agent.ProcessActivity,
	phase agent.ProcessPhase,
	toolStatus ToolStatus,
	resultErr bool,
) agent.OperatorState {
	// Card-level status overrides everything.
	if state, ok := stateFromToolStatus(toolStatus); ok {
		return state
	}

	if resultErr {
		return agent.OpStateFailed
	}

	if phase == agent.PhaseThinking {
		return agent.OpStateThinking
	}

	if len(activity) == 0 {
		return agent.OpStateIdle
	}
	return stateFromActivity(activity[len(activity)-1])
}

// stateFromToolStatus maps terminal ToolStatus values to OperatorState.
func stateFromToolStatus(ts ToolStatus) (agent.OperatorState, bool) {
	switch ts {
	case ToolStatusAwaitingPermission:
		return agent.OpStateWaitingPermission, true
	case ToolStatusCanceled:
		return agent.OpStateCanceled, true
	case ToolStatusError:
		return agent.OpStateFailed, true
	case ToolStatusSuccess:
		return agent.OpStateDone, true
	default:
		return "", false
	}
}

// stateFromActivity derives operator state from a single activity entry.
func stateFromActivity(act agent.ProcessActivity) agent.OperatorState {
	switch act.Kind {
	case agent.ActivityError:
		return agent.OpStateFailed
	case agent.ActivityThinking:
		return agent.OpStateThinking
	case agent.ActivityReplacement:
		return agent.OpStateRunning
	case agent.ActivityTool:
		return classifyToolName(act.Name, act.Detail)
	default:
		return agent.OpStateIdle
	}
}

// classifyToolName maps a tool name to an operator state.
func classifyToolName(name, detail string) agent.OperatorState {
	switch strings.ToLower(name) {
	case toolView, toolRead:
		return agent.OpStateReading
	case toolWrite, toolEdit, toolMultiedit:
		return agent.OpStateEditing
	case toolBash:
		if isTestCommand(detail) {
			return agent.OpStateTesting
		}
		return agent.OpStateRunning
	case toolGlob, toolGrep, toolLS, toolWebSearch, toolSourcegraph:
		return agent.OpStateSearching
	default:
		// ACP backends send human-readable titles as tool names.
		// Try keyword-based classification before falling back to Running.
		if isHumanReadableTitle(name) {
			return classifyByKeywords(strings.ToLower(name))
		}
		return agent.OpStateRunning
	}
}

// isHumanReadableTitle reports whether a tool name looks like a human-readable
// title (from ACP) rather than a machine tool name (from Claude CLI).
// Heuristic: contains spaces or exceeds the typical machine name length.
func isHumanReadableTitle(name string) bool {
	return strings.Contains(name, " ") || len(name) > 20
}

// classifyByKeywords infers an operator state from keywords in a human-readable
// tool title. Returns OpStateRunning if no keywords match.
func classifyByKeywords(lower string) agent.OperatorState {
	switch {
	case strings.Contains(lower, "read") || strings.Contains(lower, "view") || strings.Contains(lower, "fetch"):
		return agent.OpStateReading
	case strings.Contains(lower, "edit") || strings.Contains(lower, "write") || strings.Contains(lower, "create") || strings.Contains(lower, "update"):
		return agent.OpStateEditing
	case strings.Contains(lower, "search") || strings.Contains(lower, "find") || strings.Contains(lower, "list") || strings.Contains(lower, "grep"):
		return agent.OpStateSearching
	case strings.Contains(lower, "test"):
		return agent.OpStateTesting
	default:
		return agent.OpStateRunning
	}
}

// ComputeSummary computes a StationSummary from live data.
func ComputeSummary(
	activity []agent.ProcessActivity,
	phase agent.ProcessPhase,
	toolStatus ToolStatus,
	result *message.ToolResult,
) StationSummary {
	var resultErr bool
	if result != nil {
		resultErr = result.IsError
	}

	s := StationSummary{
		State: DeriveOperatorState(activity, phase, toolStatus, resultErr),
	}

	readPaths := map[string]bool{}
	editPaths := map[string]bool{}

	for _, act := range activity {
		countActivity(&s, act, readPaths, editPaths)
	}
	s.FilesRead += len(readPaths)
	s.FilesEdited += len(editPaths)

	if len(activity) > 0 {
		s.LastAction = translateActivity(activity[len(activity)-1])
	}

	s.ResultLine = extractResultLine(result)
	return s
}

// countActivity accumulates a single activity entry into summary counters.
func countActivity(s *StationSummary, act agent.ProcessActivity, readPaths, editPaths map[string]bool) {
	switch act.Kind {
	case agent.ActivityError:
		s.Errors++
	case agent.ActivityTool:
		countToolActivity(s, act, readPaths, editPaths)
	}
}

// countToolActivity counts metrics for a single tool activity.
func countToolActivity(s *StationSummary, act agent.ProcessActivity, readPaths, editPaths map[string]bool) {
	lower := strings.ToLower(act.Name)
	switch lower {
	case toolView, toolRead:
		countFilePath(act.Detail, readPaths, &s.FilesRead)
	case toolWrite, toolEdit, toolMultiedit:
		countFilePath(act.Detail, editPaths, &s.FilesEdited)
	case toolBash:
		s.CommandsRun++
		if isTestCommand(act.Detail) {
			s.TestsRun++
		}
	default:
		if isHumanReadableTitle(act.Name) {
			countByKeywords(s, classifyByKeywords(lower))
		}
	}
}

// countFilePath deduplicates a file path into the set, or increments the
// fallback counter when no path is available.
func countFilePath(detail string, paths map[string]bool, fallback *int) {
	if p := cleanDetail(detail); p != "" {
		paths[p] = true
	} else {
		*fallback++
	}
}

// countByKeywords increments summary counters based on a keyword-classified state.
func countByKeywords(s *StationSummary, state agent.OperatorState) {
	switch state {
	case agent.OpStateReading:
		s.FilesRead++
	case agent.OpStateEditing:
		s.FilesEdited++
	case agent.OpStateTesting:
		s.CommandsRun++
		s.TestsRun++
	case agent.OpStateRunning:
		s.CommandsRun++
	}
}

// cleanDetail strips the left-truncation prefix and returns a normalized path.
func cleanDetail(detail string) string {
	detail = strings.TrimPrefix(detail, "…")
	return strings.TrimSpace(detail)
}

// translateActivity converts a raw ProcessActivity into an operator-friendly label.
func translateActivity(act agent.ProcessActivity) string {
	switch act.Kind {
	case agent.ActivityError:
		if act.Detail != "" {
			return "Error: " + truncateDetail(act.Detail, 80)
		}
		return "Error: " + act.Name
	case agent.ActivityThinking:
		if act.Detail == "" {
			return "Thinking"
		}
		return act.Detail
	case agent.ActivityReplacement:
		if act.Detail != "" {
			return act.Detail
		}
		return "Replaced"
	case agent.ActivityTool:
		return translateToolActivity(act.Name, act.Detail)
	}
	return act.Name
}

// toolTranslation defines how to translate a tool name into an operator label.
type toolTranslation struct {
	verb     string
	usePath  bool   // use filepath.Base(detail) instead of raw detail
	fallback string // shown when detail is empty
	truncLen int    // if > 0, truncate detail to this length
}

// toolTranslations maps lowercase tool names to their translation rules.
var toolTranslations = map[string]toolTranslation{
	toolView:        {verb: "Reading", usePath: true, fallback: "file"},
	toolRead:        {verb: "Reading", usePath: true, fallback: "file"},
	toolWrite:       {verb: "Creating", usePath: true, fallback: "file"},
	toolEdit:        {verb: "Editing", usePath: true, fallback: "file"},
	toolMultiedit:   {verb: "Editing", usePath: true, fallback: "file"},
	toolGlob:        {verb: "Searching for", fallback: "files"},
	toolGrep:        {verb: "Searching for", fallback: "files"},
	toolLS:          {verb: "Listing", usePath: true, fallback: "directory"},
	toolWebSearch:   {verb: "Searching:", fallback: "web"},
	toolWebFetch:    {verb: "Fetching", truncLen: 40, fallback: "URL"},
	toolFetch:       {verb: "Fetching", truncLen: 40, fallback: "URL"},
	toolAgenticF:    {verb: "Fetching:", truncLen: 40, fallback: "URL"},
	toolDownload:    {verb: "Downloading", truncLen: 40},
	toolSourcegraph: {verb: "Code searching:"},
	toolTodos:       {verb: "Updating tasks"},
	toolAgent:       {verb: "Delegating subtask"},
	toolJobOutput:   {verb: "Checking background job"},
}

// translateToolActivity maps a tool name + detail to an operator-facing label.
func translateToolActivity(name, detail string) string {
	lower := strings.ToLower(name)

	if lower == toolBash {
		return classifyBashCommand(detail)
	}

	if tr, ok := toolTranslations[lower]; ok {
		return applyTranslation(tr, detail)
	}

	return translateToolDefault(name, lower, detail)
}

// applyTranslation renders a toolTranslation with the given detail.
func applyTranslation(tr toolTranslation, detail string) string {
	d := detail
	if tr.usePath {
		d = detailBasename(detail)
	} else if tr.truncLen > 0 && d != "" {
		d = truncateDetail(d, tr.truncLen)
	}
	return withDetail(tr.verb, d, tr.fallback)
}

// translateToolDefault handles MCP, ACP human-readable titles, and unknown tools.
func translateToolDefault(name, lower, detail string) string {
	if strings.HasPrefix(lower, "mcp_") {
		return "Using " + name
	}
	// ACP human-readable titles are already descriptive — pass through as-is.
	if isHumanReadableTitle(name) {
		return truncateDetail(name, 50)
	}
	if detail != "" {
		return name + " " + truncateDetail(detail, 30)
	}
	return name
}

// withDetail builds "verb detail" or "verb fallback" when detail is empty.
func withDetail(verb, detail, fallback string) string {
	if detail != "" {
		return verb + " " + detail
	}
	if fallback != "" {
		return verb + " " + fallback
	}
	return verb
}

// classifyBashCommand inspects a bash command detail and returns an operator label.
func classifyBashCommand(detail string) string {
	d := strings.ToLower(detail)
	switch {
	case isTestCommand(detail):
		return "Running tests"
	case strings.HasPrefix(d, "git "):
		if parts := strings.Fields(detail); len(parts) >= 2 {
			return "Running git " + parts[1]
		}
		return "Running git"
	case hasCmdPrefix(d, "npm install", "yarn install", "pip install", "go mod"):
		return "Installing deps"
	case hasCmdPrefix(d, "go build", "make ", "cargo build", "npm run build"):
		return "Building"
	default:
		if detail == "" {
			return "Running command"
		}
		return "Running: " + truncateDetail(detail, 40)
	}
}

// hasCmdPrefix reports whether s starts with any of the given command prefixes
// (exact match or followed by a space).
func hasCmdPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if s == p || strings.HasPrefix(s, p+" ") {
			return true
		}
	}
	return false
}

// summarizeActivity collapses consecutive similar activities into summary lines.
func summarizeActivity(activities []agent.ProcessActivity) []string {
	if len(activities) == 0 {
		return nil
	}

	var result []string
	i := 0
	for i < len(activities) {
		if collapsed, n := tryCollapseRun(activities, i); n > 0 {
			result = append(result, collapsed)
			i += n
		} else {
			result = append(result, translateActivity(activities[i]))
			i++
		}
	}
	return result
}

// collapseKind identifies which collapse group a tool activity belongs to.
type collapseKind int

const (
	collapseNone collapseKind = iota
	collapseRead
	collapseSearch
	collapseEdit
)

// actCollapseKind returns which collapse group an activity belongs to.
func actCollapseKind(act agent.ProcessActivity) collapseKind {
	if act.Kind != agent.ActivityTool {
		return collapseNone
	}
	lower := strings.ToLower(act.Name)
	switch {
	case isReadToolName(lower):
		return collapseRead
	case isSearchToolName(lower):
		return collapseSearch
	case isEditToolName(lower):
		return collapseEdit
	default:
		return collapseNone
	}
}

// tryCollapseRun attempts to collapse a run of similar activities starting at index.
// Returns the collapsed string and the number of entries consumed, or ("", 0) if no collapse.
func tryCollapseRun(activities []agent.ProcessActivity, start int) (string, int) {
	kind := actCollapseKind(activities[start])
	if kind == collapseNone {
		return "", 0
	}

	n := 1
	// For edits, compare full cleaned detail (not basename) to avoid merging
	// edits to different files that share the same filename.
	detail := cleanDetail(activities[start].Detail)
	for j := start + 1; j < len(activities); j++ {
		if actCollapseKind(activities[j]) != kind {
			break
		}
		if kind == collapseEdit && cleanDetail(activities[j].Detail) != detail {
			break
		}
		n++
	}

	return formatCollapsed(kind, n, detailBasename(activities[start].Detail))
}

// formatCollapsed renders the collapsed summary for a run of activities.
func formatCollapsed(kind collapseKind, n int, base string) (string, int) {
	switch kind {
	case collapseRead:
		if n >= 3 {
			return fmt.Sprintf("Reading %d files", n), n
		}
	case collapseSearch:
		if n >= 3 {
			return fmt.Sprintf("Searching (%d queries)", n), n
		}
	case collapseEdit:
		if n >= 2 && base != "" {
			return fmt.Sprintf("Editing %s (%d changes)", base, n), n
		}
	}
	return "", 0
}

// extractResultLine extracts the first useful body line from a station result,
// skipping markdown headings, blank lines, and formatting prefixes.
func extractResultLine(result *message.ToolResult) string {
	if result == nil || result.Content == "" {
		return ""
	}
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip markdown headings (lines starting with #).
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Strip leading list/quote markers.
		line = strings.TrimLeft(line, "*")
		line = strings.TrimLeft(line, "-")
		line = strings.TrimLeft(line, ">")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 80 {
			return line[:77] + "…"
		}
		return line
	}
	return ""
}

// stateChipStyle returns the lipgloss style for an operator state chip.
func stateChipStyle(sty *styles.Styles, state agent.OperatorState) lipgloss.Style {
	switch state {
	case agent.OpStateFailed:
		return sty.Tool.StationChipError
	case agent.OpStateWaitingPermission:
		return sty.Tool.StationChipWarning
	case agent.OpStateCanceled:
		return sty.Tool.StateCancelled
	case agent.OpStateEditing, agent.OpStateTesting:
		return sty.HalfMuted
	case agent.OpStateThinking:
		return sty.Muted
	default:
		return sty.Subtle
	}
}

// --- helpers ---

func isReadToolName(lower string) bool {
	return lower == toolView || lower == toolRead
}

func isEditToolName(lower string) bool {
	return lower == toolWrite || lower == toolEdit || lower == toolMultiedit
}

func isSearchToolName(lower string) bool {
	return lower == toolGrep || lower == toolGlob
}

// testRunnerPrefixes are command prefixes that indicate a test run.
var testRunnerPrefixes = []string{
	"go test", "cargo test", "npm test", "yarn test", "npm run test",
	"pytest", "jest", "vitest", "phpunit", "rspec", "mocha",
	"make test", "make check",
}

func isTestCommand(detail string) bool {
	d := strings.ToLower(strings.TrimSpace(detail))
	for _, prefix := range testRunnerPrefixes {
		if d == prefix || strings.HasPrefix(d, prefix+" ") {
			return true
		}
	}
	return false
}

func detailBasename(detail string) string {
	detail = strings.TrimPrefix(detail, "…")
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	return filepath.Base(detail)
}

func truncateDetail(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}
