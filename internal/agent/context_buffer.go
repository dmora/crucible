package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dmora/agentrun"
)

// Buffer tuning constants.
const (
	exhaustionThreshold = 0.85 // 85% of context capacity
	maxGenerations      = 3    // up to 3 replacements (4 total attempts including original)
	maxBufferTurns      = 50
	maxInputSummaryLen  = 100
	maxOutputSummaryLen = 200
	maxThinkingLen      = 500

	fileOpWrite = "write"
	fileOpEdit  = "edit"
)

// BufferedToolCall captures one tool invocation for the continuation prompt.
type BufferedToolCall struct {
	Name          string
	InputSummary  string // ≤maxInputSummaryLen chars
	OutputSummary string // ≤maxOutputSummaryLen chars
}

// FileOp captures a file modification observed during a turn.
type FileOp struct {
	Path string
	Op   string // "read", "edit", "write", "create"
}

// TurnRecord captures one RunTurn invocation's data.
type TurnRecord struct {
	Seq              int
	ToolCalls        []BufferedToolCall
	Files            []FileOp
	Errors           []string
	ResultText       string // finalized result (snapshot from builder)
	Thinking         string // last thinking content, capped at maxThinkingLen
	LastTodoSnapshot json.RawMessage
	resultBuf        strings.Builder
	Timestamp        time.Time
	Elapsed          time.Duration
	ContextUsed      int
	ContextSize      int
	pendingFileOp    *FileOp // set at tool start, confirmed or cleared at tool result
}

// ContextBuffer captures structured data from station messages for continuation prompts.
type ContextBuffer struct {
	turns        []TurnRecord
	generation   int    // 0 = original, 1 = first replacement, ...
	originalTask string // the initial task prompt
	current      *TurnRecord
	repoState    string // git status/diff captured at replacement time
}

// NewContextBuffer creates a new ContextBuffer for a station session.
func NewContextBuffer(originalTask string) *ContextBuffer {
	return &ContextBuffer{
		originalTask: originalTask,
	}
}

// StartTurn begins a new TurnRecord.
func (cb *ContextBuffer) StartTurn() {
	cb.current = &TurnRecord{
		Seq:       len(cb.turns),
		Timestamp: time.Now(),
	}
}

// RecordToolStart appends a new BufferedToolCall to the current turn.
// For Write/Edit tools, stores a pending FileOp that is confirmed on tool
// success (see ConfirmPendingFileOp). This ensures only successful writes
// are recorded.
// TodoWrite inputs are captured as the latest task progress snapshot.
func (cb *ContextBuffer) RecordToolStart(name, input string) {
	if cb.current == nil {
		return
	}
	cb.current.ToolCalls = append(cb.current.ToolCalls, BufferedToolCall{
		Name:         name,
		InputSummary: truncate(input, maxInputSummaryLen),
	})

	// Capture TodoWrite input as the latest task progress snapshot.
	if strings.EqualFold(name, "TodoWrite") {
		cb.current.LastTodoSnapshot = json.RawMessage(input)
	}

	// Store pending FileOp — confirmed on tool success, cleared on failure.
	// Parsed here because msg.Tool.Input is not available on MessageToolResult
	// for all backends (e.g. ACP).
	// Only overwrite pendingFileOp when the current tool IS a file op.
	// Non-file tools (Read, Bash, etc.) must not clear a pending Write/Edit.
	if op := inferFileOp(name); op != "" {
		fields := parseJSONFields(input)
		path := fields["file_path"]
		if path == "" {
			path = fields["path"]
		}
		if path != "" {
			cb.current.pendingFileOp = &FileOp{Path: path, Op: op}
			slog.Debug("FileOp pending", "tool", name, "op", op, "path", path)
		}
	} else {
		slog.Debug("FileOp skip (not a file tool)", "tool", name)
	}
}

// inferFileOp returns the file operation string for known file-modifying tools, or "".
// Read/View are excluded — reads aren't modifications and add noise.
// Covers both Claude CLI tool names and ACP/OpenCode variants.
func inferFileOp(name string) string {
	switch strings.ToLower(name) {
	case "write", "write_file":
		return fileOpWrite
	case "edit", "multiedit", "edit_file", "file_edit", "str_replace_editor":
		return fileOpEdit
	default:
		return ""
	}
}

// RecordToolOutput attaches a truncated OutputSummary to the most recent
// BufferedToolCall in the current turn. No-op if no pending tool call.
func (cb *ContextBuffer) RecordToolOutput(output string) {
	if cb.current == nil || len(cb.current.ToolCalls) == 0 {
		return
	}
	last := &cb.current.ToolCalls[len(cb.current.ToolCalls)-1]
	last.OutputSummary = truncate(output, maxOutputSummaryLen)
}

// ConfirmPendingFileOp moves the pending FileOp (set at tool start) into the
// confirmed Files list. Called when a tool result indicates success.
// toolName is the name of the tool whose result just completed. The pending op
// is only confirmed when the completing tool is itself a file-modifying tool,
// preventing a Read result from incorrectly confirming a pending Write.
// Pass "" for unconditional confirmation (hard-error fallback).
func (cb *ContextBuffer) ConfirmPendingFileOp(toolName string) {
	if cb.current == nil || cb.current.pendingFileOp == nil {
		slog.Debug("FileOp confirm skip", "tool", toolName, "reason", "no pending op")
		return
	}
	if toolName != "" && inferFileOp(toolName) == "" {
		slog.Debug("FileOp confirm skip", "tool", toolName, "reason", "non-file tool result")
		return // non-file tool result — leave pending op alone
	}
	slog.Debug("FileOp confirmed", "tool", toolName, "path", cb.current.pendingFileOp.Path, "op", cb.current.pendingFileOp.Op)
	cb.RecordFileOp(cb.current.pendingFileOp.Path, cb.current.pendingFileOp.Op)
	cb.current.pendingFileOp = nil
}

// ClearPendingFileOp discards the pending FileOp without recording it.
// Called when a tool result indicates failure.
// toolName is the name of the tool whose result just completed. The pending op
// is only cleared when the completing tool is a file-modifying tool or when
// toolName is "" (unconditional clear for hard errors like MessageError).
func (cb *ContextBuffer) ClearPendingFileOp(toolName string) {
	if cb.current == nil {
		return
	}
	if toolName != "" && inferFileOp(toolName) == "" {
		return // non-file tool result — leave pending op alone
	}
	cb.current.pendingFileOp = nil
}

// RecordFileOp appends a file operation to the current turn, deduplicated by path+op.
func (cb *ContextBuffer) RecordFileOp(path, op string) {
	if cb.current == nil {
		return
	}
	for _, f := range cb.current.Files {
		if f.Path == path && f.Op == op {
			return // deduplicate
		}
	}
	cb.current.Files = append(cb.current.Files, FileOp{Path: path, Op: op})
}

// RecordError appends an error to the current turn (full text, no truncation).
func (cb *ContextBuffer) RecordError(errText string) {
	if cb.current == nil {
		return
	}
	cb.current.Errors = append(cb.current.Errors, errText)
}

// RecordThinking captures the last thinking content for the current turn.
// Only the most recent thinking is kept (most relevant at interruption).
func (cb *ContextBuffer) RecordThinking(content string) {
	if cb.current == nil {
		return
	}
	cb.current.Thinking = truncate(content, maxThinkingLen)
}

// AppendResult appends a chunk to the current turn's result (streaming-safe).
func (cb *ContextBuffer) AppendResult(chunk string) {
	if cb.current == nil {
		return
	}
	cb.current.resultBuf.WriteString(chunk)
}

// RecordContextFill sets the current turn's context metrics.
func (cb *ContextBuffer) RecordContextFill(used, size int) {
	if cb.current == nil {
		return
	}
	if used > cb.current.ContextUsed {
		cb.current.ContextUsed = used
	}
	if size > 0 {
		cb.current.ContextSize = size
	}
}

// FinalizeTurn finalizes the current turn and appends it to the turns slice.
func (cb *ContextBuffer) FinalizeTurn(elapsed time.Duration) {
	if cb.current == nil {
		return
	}
	cb.current.Elapsed = elapsed
	cb.current.ResultText = cb.current.resultBuf.String()

	cb.turns = append(cb.turns, *cb.current)
	cb.current = nil

	// Enforce maxBufferTurns cap.
	if len(cb.turns) > maxBufferTurns {
		cb.turns = cb.turns[len(cb.turns)-maxBufferTurns:]
	}
}

// Generation returns the current generation number.
func (cb *ContextBuffer) Generation() int {
	return cb.generation
}

// IncrementGeneration bumps the generation counter.
func (cb *ContextBuffer) IncrementGeneration() {
	cb.generation++
}

// SetRepoState sets the git repo state snapshot for the continuation prompt.
func (cb *ContextBuffer) SetRepoState(state string) {
	cb.repoState = state
}

// exhaustionPatterns are substrings in result text that indicate context
// window exhaustion. Matched case-insensitively.
var exhaustionPatterns = []string{
	"prompt is too long",
	"context window is full",
	"max context length exceeded",
	"conversation is too long",
}

// ShouldReplace checks exhaustion signals and returns whether the station
// should be replaced, along with a reason string.
func (cb *ContextBuffer) ShouldReplace(stopReason agentrun.StopReason, err error, contextUsed, contextSize int) (bool, string) {
	// Never fight user/supervisor cancellation.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, ""
	}

	if stopReason == agentrun.StopMaxTokens {
		return true, "StopMaxTokens"
	}

	if contextSize > 0 {
		ratio := float64(contextUsed) / float64(contextSize)
		if ratio >= exhaustionThreshold {
			return true, fmt.Sprintf("threshold %.0f%%", ratio*100)
		}
	}

	// Result text exhaustion: CLI backends may surface context exhaustion as
	// normal result text (e.g., "Prompt is too long") with stop_reason=end_turn
	// instead of StopMaxTokens. Check the last finalized turn's result.
	if len(cb.turns) > 0 {
		lastResult := strings.ToLower(cb.turns[len(cb.turns)-1].ResultText)
		for _, pattern := range exhaustionPatterns {
			if strings.Contains(lastResult, pattern) {
				return true, fmt.Sprintf("result text: %s", pattern)
			}
		}
	}

	// Crash recovery: process error with buffered work.
	if err != nil && len(cb.turns) > 0 {
		return true, "process error with buffered work"
	}

	return false, ""
}

// AtGenerationCap returns true if the generation limit has been reached.
func (cb *ContextBuffer) AtGenerationCap() bool {
	return cb.generation >= maxGenerations
}

// temporalBands computes the summary/detail/recent band boundaries.
func temporalBands(n int) (summaryEnd, detailEnd int) {
	switch {
	case n <= 5:
		return 0, 0
	case n <= 15:
		mid := n / 2
		return mid, mid
	default:
		summaryEnd = n * 60 / 100
		recentCount := max(n*15/100, 3)
		return summaryEnd, n - recentCount
	}
}

// BuildContinuationPrompt constructs an XML continuation prompt from buffered data.
// The <mission> section appears first so the replacement station sees the primary
// objective before any work history. A <replacement_notice> instructs it to re-read
// referenced files that were in the original station's context.
func (cb *ContextBuffer) BuildContinuationPrompt() string {
	var b strings.Builder

	b.WriteString("<continuation_context>\n")

	fmt.Fprintf(&b, "<mission>%s</mission>\n\n", cb.originalTask)
	b.WriteString(`<replacement_notice>
You are a REPLACEMENT station. A previous instance worked on this task but
exhausted its context window. The sections below describe what it accomplished.
IMPORTANT: Re-read any plan files, design documents, or specs referenced in
the mission above before proceeding — the previous instance had these in its
context but they are not included in this handoff.
</replacement_notice>
`)

	n := len(cb.turns)
	summaryEnd, detailEnd := temporalBands(n)

	writeBandSummary(&b, cb.turns[:summaryEnd])
	writeBandDetail(&b, cb.turns[summaryEnd:detailEnd])
	writeBandRecent(&b, cb.turns[max(summaryEnd, detailEnd):])
	writeTaskProgress(&b, cb.turns)
	writeFailedApproaches(&b, cb.turns)

	if cb.repoState != "" {
		fmt.Fprintf(&b, "  <repo_state>\n%s\n  </repo_state>\n", cb.repoState)
	}

	b.WriteString("</continuation_context>\n\n")
	b.WriteString("Your primary goal is the <mission> above. Use the work history sections to\nunderstand what was already done. Do not repeat completed work or re-attempt\nfailed approaches. Start by re-reading any files referenced in the mission.")

	return b.String()
}

// writeBandSummary writes the summary band (compact, oldest turns).
func writeBandSummary(b *strings.Builder, turns []TurnRecord) {
	if len(turns) == 0 {
		return
	}
	b.WriteString("  <previous_work_summary>\n")
	for i := range turns {
		writeTurnSummary(b, &turns[i])
	}
	b.WriteString("  </previous_work_summary>\n")
}

// writeBandDetail writes the detail band (middle turns).
func writeBandDetail(b *strings.Builder, turns []TurnRecord) {
	if len(turns) == 0 {
		return
	}
	b.WriteString("  <previous_work_detail>\n")
	for i := range turns {
		writeTurnDetail(b, &turns[i])
	}
	b.WriteString("  </previous_work_detail>\n")
}

// writeBandRecent writes the recent band (most recent turns, full detail).
func writeBandRecent(b *strings.Builder, turns []TurnRecord) {
	if len(turns) == 0 {
		return
	}
	b.WriteString("  <recent_activity>\n")
	for i := range turns {
		writeTurnDetail(b, &turns[i])
	}
	b.WriteString("  </recent_activity>\n")
}

// writeFailedApproaches collects all errors and writes the failed_approaches section.
func writeFailedApproaches(b *strings.Builder, turns []TurnRecord) {
	var count int
	for i := range turns {
		count += len(turns[i].Errors)
	}
	if count == 0 {
		return
	}
	errs := make([]string, 0, count)
	for i := range turns {
		errs = append(errs, turns[i].Errors...)
	}
	b.WriteString("  <failed_approaches>\n")
	for _, e := range errs {
		fmt.Fprintf(b, "    - %s\n", e)
	}
	b.WriteString("  </failed_approaches>\n")
}

// writeTurnSummary writes a compact summary for a turn (summary band).
func writeTurnSummary(b *strings.Builder, t *TurnRecord) {
	fmt.Fprintf(b, "    Turn %d:", t.Seq)
	for _, tc := range t.ToolCalls {
		writeToolSummary(b, tc)
	}
	if len(t.Errors) > 0 {
		fmt.Fprintf(b, " ERRORS: %d;", len(t.Errors))
	}
	b.WriteString("\n")
}

// writeToolSummary writes a single tool call in semantic summary form.
func writeToolSummary(b *strings.Builder, tc BufferedToolCall) {
	name := strings.ToLower(tc.Name)
	switch name {
	case "read", "view":
		fmt.Fprintf(b, " Read %s;", extractPathFromJSON(tc.InputSummary))
	case "edit", "multiedit":
		fmt.Fprintf(b, " Edited %s;", extractPathFromJSON(tc.InputSummary))
	case "write":
		fmt.Fprintf(b, " Wrote %s;", extractPathFromJSON(tc.InputSummary))
	case "bash":
		fmt.Fprintf(b, " Ran `%s`;", extractJSONField(tc.InputSummary, "command"))
	case "grep":
		fmt.Fprintf(b, " Searched for '%s';", extractJSONField(tc.InputSummary, "pattern"))
	case "glob":
		fmt.Fprintf(b, " Glob %s;", extractJSONField(tc.InputSummary, "pattern"))
	case "todowrite", "thinking":
		// skip — station-internal bookkeeping
	case "agent":
		fmt.Fprintf(b, " Agent: %s;", extractJSONField(tc.InputSummary, "description"))
	default:
		// Include truncated payload so tools like web_search, ask_user retain context.
		fmt.Fprintf(b, " %s: %s;", tc.Name, tc.InputSummary)
	}
}

// extractPathFromJSON extracts file_path or path from a JSON input summary.
// Falls back to the raw input if neither field is found.
func extractPathFromJSON(input string) string {
	fields := parseJSONFields(input)
	if p, ok := fields["file_path"]; ok {
		return p
	}
	if p, ok := fields["path"]; ok {
		return p
	}
	return input // fallback to raw summary
}

// extractJSONField extracts a single string field from a JSON input summary.
// Falls back to the raw input if the field is missing or parsing fails.
func extractJSONField(input, field string) string {
	fields := parseJSONFields(input)
	if v, ok := fields[field]; ok {
		return v
	}
	return input // fallback
}

// parseJSONFields attempts to parse the input as JSON and returns string values.
// Handles truncated JSON gracefully by trying progressively simpler repairs.
// Returns an empty map if parsing fails entirely.
func parseJSONFields(input string) map[string]string {
	result := make(map[string]string)

	var raw map[string]any
	// Try direct parse first (valid JSON).
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		// InputSummary is often truncated — try closing the JSON.
		for _, suffix := range []string{`"}`, `"}`} {
			if err := json.Unmarshal([]byte(input+suffix), &raw); err == nil {
				break
			}
		}
	}

	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// writeTurnDetail writes full detail for a turn (detail/recent band).
func writeTurnDetail(b *strings.Builder, t *TurnRecord) {
	fmt.Fprintf(b, "    Turn %d:\n", t.Seq)
	for _, tc := range t.ToolCalls {
		fmt.Fprintf(b, "      - %s(%s)", tc.Name, tc.InputSummary)
		if tc.OutputSummary != "" {
			fmt.Fprintf(b, " -> %s", tc.OutputSummary)
		}
		b.WriteString("\n")
	}
	// Per-turn file listings omitted — aggregated <modified_files> is sufficient.
	if t.Thinking != "" {
		fmt.Fprintf(b, "      [thinking] %s\n", t.Thinking)
	}
	for _, e := range t.Errors {
		fmt.Fprintf(b, "      [error] %s\n", e)
	}
	if t.ResultText != "" {
		fmt.Fprintf(b, "      [result] %s\n", t.ResultText)
	}
}

// writeTaskProgress scans turns backward for the latest TodoWrite snapshot
// and renders it as a <task_progress> XML block with a formatted checklist.
// No-op if no snapshot exists.
func writeTaskProgress(b *strings.Builder, turns []TurnRecord) {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].LastTodoSnapshot != nil {
			rendered := renderTaskProgress(turns[i].LastTodoSnapshot)
			fmt.Fprintf(b, "  <task_progress>\n%s  </task_progress>\n", rendered)
			return
		}
	}
}

// todoPayload is the expected shape of a TodoWrite tool input.
type todoPayload struct {
	Todos []todoItem `json:"todos"`
}

type todoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// renderTaskProgress parses a TodoWrite JSON payload and returns a formatted
// checklist. Falls back to raw content if parsing fails.
func renderTaskProgress(raw json.RawMessage) string {
	var payload todoPayload
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Todos) == 0 {
		// Malformed or unexpected shape — render verbatim.
		return string(raw) + "\n"
	}
	var b strings.Builder
	for _, item := range payload.Todos {
		marker := "[ ]"
		switch item.Status {
		case "completed":
			marker = "[x]"
		case "in_progress":
			marker = "[~]"
		}
		fmt.Fprintf(&b, "    - %s %s\n", marker, item.Content)
	}
	return b.String()
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// LastWrittenMD returns the path of the last confirmed Write targeting a .md
// file across all finalized turns. Returns "" if none found.
func (cb *ContextBuffer) LastWrittenMD() string {
	var last string
	for _, t := range cb.turns {
		for _, f := range t.Files {
			slog.Debug("LastWrittenMD scan", "path", f.Path, "op", f.Op)
			if f.Op == fileOpWrite && strings.HasSuffix(f.Path, ".md") {
				last = f.Path
			}
		}
	}
	slog.Debug("LastWrittenMD result", "path", last, "turns", len(cb.turns))
	return last
}
