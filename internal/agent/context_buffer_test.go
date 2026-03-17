package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmora/agentrun"
)

func TestBufferCapture(t *testing.T) {
	cb := NewContextBuffer("implement feature X")
	cb.StartTurn()
	cb.RecordToolStart("Read", `{"file_path":"/src/main.go"}`)
	cb.RecordToolOutput("package main\nfunc main() {}")
	cb.RecordError("compile error: undefined foo")
	cb.AppendResult("I read the file")
	cb.RecordContextFill(50000, 200000)
	cb.FinalizeTurn(2 * time.Second)

	if len(cb.turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(cb.turns))
	}
	turn := cb.turns[0]
	if turn.Seq != 0 {
		t.Errorf("expected Seq=0, got %d", turn.Seq)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "Read" {
		t.Errorf("expected tool name 'Read', got %q", turn.ToolCalls[0].Name)
	}
	if turn.ToolCalls[0].OutputSummary == "" {
		t.Error("expected OutputSummary to be set")
	}
	// Read no longer records a FileOp (reads aren't modifications).
	if len(turn.Files) != 0 {
		t.Errorf("expected 0 file ops for Read, got %d", len(turn.Files))
	}
	if len(turn.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(turn.Errors))
	}
	if turn.ResultText != "I read the file" {
		t.Errorf("unexpected ResultText: %q", turn.ResultText)
	}
	if turn.ContextUsed != 50000 {
		t.Errorf("expected ContextUsed=50000, got %d", turn.ContextUsed)
	}
	if turn.ContextSize != 200000 {
		t.Errorf("expected ContextSize=200000, got %d", turn.ContextSize)
	}
}

func TestAppendResultAccumulates(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.AppendResult("chunk1")
	cb.AppendResult(" chunk2")
	cb.AppendResult(" chunk3")
	cb.FinalizeTurn(time.Second)

	if cb.turns[0].ResultText != "chunk1 chunk2 chunk3" {
		t.Errorf("expected accumulated result, got %q", cb.turns[0].ResultText)
	}
}

func TestRecordToolOutputAttachesToLastTool(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordToolStart("Read", `{"file_path":"foo.go"}`)
	cb.RecordToolOutput("file contents here")
	cb.FinalizeTurn(time.Second)

	tc := cb.turns[0].ToolCalls[0]
	if tc.OutputSummary != "file contents here" {
		t.Errorf("expected output attached, got %q", tc.OutputSummary)
	}
}

func TestRecordToolOutputNoOpWithoutPendingTool(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	// Should not panic.
	cb.RecordToolOutput("orphaned output")
	cb.FinalizeTurn(time.Second)

	if len(cb.turns[0].ToolCalls) != 0 {
		t.Error("expected no tool calls")
	}
}

func TestTurnCap(t *testing.T) {
	cb := NewContextBuffer("task")
	for i := 0; i < maxBufferTurns+10; i++ {
		cb.StartTurn()
		cb.FinalizeTurn(time.Millisecond)
	}
	if len(cb.turns) != maxBufferTurns {
		t.Errorf("expected %d turns, got %d", maxBufferTurns, len(cb.turns))
	}
}

func TestInputOutputTruncation(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()

	longInput := strings.Repeat("x", maxInputSummaryLen+50)
	longOutput := strings.Repeat("y", maxOutputSummaryLen+50)
	cb.RecordToolStart("Bash", longInput)
	cb.RecordToolOutput(longOutput)
	cb.FinalizeTurn(time.Second)

	tc := cb.turns[0].ToolCalls[0]
	if len(tc.InputSummary) > maxInputSummaryLen {
		t.Errorf("InputSummary too long: %d", len(tc.InputSummary))
	}
	if len(tc.OutputSummary) > maxOutputSummaryLen {
		t.Errorf("OutputSummary too long: %d", len(tc.OutputSummary))
	}
}

func TestShouldReplaceStopMaxTokens(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, reason := cb.ShouldReplace(agentrun.StopMaxTokens, nil, 0, 0)
	if !replace {
		t.Error("expected replacement for StopMaxTokens")
	}
	if reason != "StopMaxTokens" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldReplaceThreshold(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	// 86% — should trigger.
	replace, _ := cb.ShouldReplace("", nil, 172000, 200000)
	if !replace {
		t.Error("expected replacement at 86%")
	}

	// 84% — should not trigger.
	replace, _ = cb.ShouldReplace("", nil, 168000, 200000)
	if replace {
		t.Error("expected no replacement at 84%")
	}
}

func TestShouldReplaceUnknownCapacity(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	// ContextSize=0 — threshold check skipped.
	replace, _ := cb.ShouldReplace("", nil, 190000, 0)
	if replace {
		t.Error("expected no replacement when capacity unknown")
	}

	// But StopMaxTokens still triggers.
	replace, _ = cb.ShouldReplace(agentrun.StopMaxTokens, nil, 190000, 0)
	if !replace {
		t.Error("expected replacement for StopMaxTokens even with unknown capacity")
	}
}

func TestShouldReplaceContextCanceled(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordContextFill(190000, 200000)
	cb.FinalizeTurn(time.Second)

	// context.Canceled should prevent replacement even with threshold exceeded.
	replace, _ := cb.ShouldReplace(agentrun.StopMaxTokens, context.Canceled, 190000, 200000)
	if replace {
		t.Error("expected no replacement on context.Canceled")
	}
}

func TestShouldReplaceDeadlineExceeded(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, _ := cb.ShouldReplace(agentrun.StopMaxTokens, context.DeadlineExceeded, 190000, 200000)
	if replace {
		t.Error("expected no replacement on DeadlineExceeded")
	}
}

func TestShouldReplaceProcessErrorWithBufferedWork(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.AppendResult("some work done")
	cb.FinalizeTurn(time.Second)

	replace, reason := cb.ShouldReplace("", errTest, 0, 0)
	if !replace {
		t.Error("expected replacement for process error with buffered work")
	}
	if !strings.Contains(reason, "process error") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldReplaceProcessErrorEmptyBuffer(t *testing.T) {
	cb := NewContextBuffer("task")

	replace, _ := cb.ShouldReplace("", errTest, 0, 0)
	if replace {
		t.Error("expected no replacement for process error with empty buffer")
	}
}

var errTest = errors.New("test error")

func TestAtGenerationCap(t *testing.T) {
	cb := NewContextBuffer("task")
	if cb.AtGenerationCap() {
		t.Error("should not be at cap initially")
	}
	for i := 0; i < maxGenerations; i++ {
		cb.IncrementGeneration()
	}
	if !cb.AtGenerationCap() {
		t.Errorf("should be at cap after %d increments", maxGenerations)
	}
}

func TestBuildContinuationPromptSmall(t *testing.T) {
	cb := NewContextBuffer("implement feature X")
	for i := 0; i < 3; i++ {
		cb.StartTurn()
		cb.RecordToolStart("Read", `{"file_path":"file.go"}`)
		cb.RecordToolOutput("contents")
		cb.AppendResult("did something")
		cb.FinalizeTurn(time.Second)
	}
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<continuation_context>") {
		t.Error("missing continuation_context tag")
	}
	if strings.Contains(prompt, "<generation>") {
		t.Error("generation tag should be stripped")
	}
	if strings.Contains(prompt, "<reason>") {
		t.Error("reason tag should be stripped")
	}
	if !strings.Contains(prompt, "<mission>implement feature X</mission>") {
		t.Error("missing mission tag")
	}
	// ≤5 turns: all in recent band, no summary band.
	if strings.Contains(prompt, "<previous_work_summary>") {
		t.Error("should not have summary band for ≤5 turns")
	}
	if !strings.Contains(prompt, "<recent_activity>") {
		t.Error("should have recent_activity for ≤5 turns")
	}
	if !strings.Contains(prompt, "Your primary goal is the <mission> above") {
		t.Error("missing continuation instruction")
	}
}

func TestBuildContinuationPromptLarge(t *testing.T) {
	cb := NewContextBuffer("big task")
	for i := 0; i < 20; i++ {
		cb.StartTurn()
		cb.RecordToolStart("Read", `{"file_path":"file.go"}`)
		if i == 5 {
			cb.RecordError("compile failed")
		}
		cb.AppendResult("step done")
		cb.FinalizeTurn(time.Second)
	}

	prompt := cb.BuildContinuationPrompt()
	// >15 turns: should have all three bands.
	if !strings.Contains(prompt, "<previous_work_summary>") {
		t.Error("missing summary band for >15 turns")
	}
	if !strings.Contains(prompt, "<recent_activity>") {
		t.Error("missing recent_activity band")
	}
}

func TestFailedApproachesSection(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordError("error one")
	cb.RecordError("error two")
	cb.FinalizeTurn(time.Second)

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<failed_approaches>") {
		t.Error("missing failed_approaches section")
	}
	if !strings.Contains(prompt, "error one") || !strings.Contains(prompt, "error two") {
		t.Error("errors not in failed_approaches")
	}
}

func TestModifiedFilesSectionStripped(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordFileOp("/src/main.go", fileOpEdit)
	cb.RecordFileOp("/src/util.go", fileOpWrite)
	cb.FinalizeTurn(time.Second)

	prompt := cb.BuildContinuationPrompt()
	if strings.Contains(prompt, "<modified_files") {
		t.Error("modified_files section should be stripped")
	}
}

func TestFileOpDeduplication(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordFileOp("/src/main.go", fileOpEdit)
	cb.RecordFileOp("/src/main.go", fileOpEdit) // duplicate
	cb.FinalizeTurn(time.Second)

	if len(cb.turns[0].Files) != 1 {
		t.Errorf("expected 1 file op after dedup, got %d", len(cb.turns[0].Files))
	}
}

// --- Step 8: Exhaustion signal detection integration tests ---

func TestShouldReplace_StopMaxTokensTriggers(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, reason := cb.ShouldReplace(agentrun.StopMaxTokens, nil, 100000, 200000)
	if !replace || reason != "StopMaxTokens" {
		t.Errorf("expected (true, StopMaxTokens), got (%v, %q)", replace, reason)
	}
}

func TestShouldReplace_ThresholdTriggers(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	// 86% triggers.
	replace, _ := cb.ShouldReplace("", nil, 172000, 200000)
	if !replace {
		t.Error("86% should trigger replacement")
	}
}

func TestShouldReplace_BelowThresholdDoesNot(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, _ := cb.ShouldReplace("", nil, 160000, 200000)
	if replace {
		t.Error("80% should not trigger replacement")
	}
}

func TestShouldReplace_UnknownCapacitySkipsThreshold(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, _ := cb.ShouldReplace("", nil, 190000, 0)
	if replace {
		t.Error("unknown capacity should skip threshold check")
	}
}

func TestShouldReplace_GenerationCapPreventsLoop(t *testing.T) {
	cb := NewContextBuffer("task")
	for i := 0; i < maxGenerations; i++ {
		cb.IncrementGeneration()
	}
	if !cb.AtGenerationCap() {
		t.Error("should be at generation cap")
	}
}

func TestShouldReplace_CancellationAbortsRecovery(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	// Even with StopMaxTokens AND threshold exceeded, cancellation wins.
	replace, _ := cb.ShouldReplace(agentrun.StopMaxTokens, context.Canceled, 190000, 200000)
	if replace {
		t.Error("cancellation should abort recovery")
	}
}

func TestShouldReplace_DeadlineExceededAbortsRecovery(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	replace, _ := cb.ShouldReplace(agentrun.StopMaxTokens, context.DeadlineExceeded, 190000, 200000)
	if replace {
		t.Error("deadline exceeded should abort recovery")
	}
}

func TestShouldReplace_ResultTextExhaustion(t *testing.T) {
	tests := []struct {
		name        string
		resultText  string
		wantReplace bool
		wantReason  string
	}{
		{
			name:        "prompt is too long",
			resultText:  "Error: Prompt is too long. Please reduce the context.",
			wantReplace: true,
			wantReason:  "result text: prompt is too long",
		},
		{
			name:        "case insensitive",
			resultText:  "PROMPT IS TOO LONG",
			wantReplace: true,
			wantReason:  "result text: prompt is too long",
		},
		{
			name:        "context window is full",
			resultText:  "The context window is full, cannot continue.",
			wantReplace: true,
			wantReason:  "result text: context window is full",
		},
		{
			name:        "conversation is too long",
			resultText:  "This conversation is too long to process.",
			wantReplace: true,
			wantReason:  "result text: conversation is too long",
		},
		{
			name:        "normal result",
			resultText:  "I successfully implemented the feature.",
			wantReplace: false,
		},
		{
			name:        "empty result",
			resultText:  "",
			wantReplace: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewContextBuffer("task")
			cb.StartTurn()
			cb.AppendResult(tt.resultText)
			cb.FinalizeTurn(time.Second)

			replace, reason := cb.ShouldReplace("", nil, 0, 0)
			if replace != tt.wantReplace {
				t.Errorf("ShouldReplace() = %v, want %v", replace, tt.wantReplace)
			}
			if tt.wantReplace && reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestShouldReplace_ResultTextNotCheckedOnCancel(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.AppendResult("Prompt is too long")
	cb.FinalizeTurn(time.Second)

	// Cancellation still wins over result text matching.
	replace, _ := cb.ShouldReplace("", context.Canceled, 0, 0)
	if replace {
		t.Error("cancellation should abort even with exhaustion result text")
	}
}

func TestRecordThinking(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordThinking("first thought")
	cb.RecordThinking("second thought") // overwrites first
	cb.FinalizeTurn(time.Second)

	if cb.turns[0].Thinking != "second thought" {
		t.Errorf("expected last thinking, got %q", cb.turns[0].Thinking)
	}
}

func TestRecordThinkingTruncation(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	long := strings.Repeat("x", maxThinkingLen+100)
	cb.RecordThinking(long)
	cb.FinalizeTurn(time.Second)

	if len(cb.turns[0].Thinking) > maxThinkingLen {
		t.Errorf("thinking too long: %d", len(cb.turns[0].Thinking))
	}
}

func TestThinkingInContinuationPrompt(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordThinking("I should refactor the parser")
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "[thinking] I should refactor the parser") {
		t.Error("thinking not in continuation prompt")
	}
}

func TestRepoStateInContinuationPrompt(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)
	cb.SetRepoState("M  src/main.go\n M src/util.go")
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<repo_state>") {
		t.Error("missing repo_state section")
	}
	if !strings.Contains(prompt, "M  src/main.go") {
		t.Error("repo state content missing")
	}
}

func TestRepoStateOmittedWhenEmpty(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if strings.Contains(prompt, "<repo_state>") {
		t.Error("repo_state should be omitted when empty")
	}
}

func TestSemanticToolSummary(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordToolStart("Read", `{"file_path":"/src/main.go"}`)
	cb.RecordToolStart("Edit", `{"file_path":"/src/main.go"}`)
	cb.RecordToolStart("Bash", `{"command":"go test ./..."}`)
	cb.RecordToolStart("Grep", `{"pattern":"TODO"}`)
	cb.RecordToolStart("TodoWrite", `{}`)
	cb.FinalizeTurn(time.Second)

	var b strings.Builder
	writeTurnSummary(&b, &cb.turns[0])
	summary := b.String()

	if !strings.Contains(summary, "Read /src/main.go") {
		t.Errorf("missing semantic Read summary in: %s", summary)
	}
	if !strings.Contains(summary, "Edited /src/main.go") {
		t.Errorf("missing semantic Edit summary in: %s", summary)
	}
	if !strings.Contains(summary, "Ran `go test ./...`") {
		t.Errorf("missing semantic Bash summary in: %s", summary)
	}
	if !strings.Contains(summary, "Searched for 'TODO'") {
		t.Errorf("missing semantic Grep summary in: %s", summary)
	}
	// TodoWrite should be omitted.
	if strings.Contains(summary, "TodoWrite") {
		t.Error("TodoWrite should be skipped in summary")
	}
}

func TestInferFileOpDropsReads(t *testing.T) {
	if op := inferFileOp("Read"); op != "" {
		t.Errorf("Read should return empty, got %q", op)
	}
	if op := inferFileOp("View"); op != "" {
		t.Errorf("View should return empty, got %q", op)
	}
	if op := inferFileOp("Edit"); op != fileOpEdit {
		t.Errorf("Edit should return %q, got %q", fileOpEdit, op)
	}
	if op := inferFileOp("Write"); op != fileOpWrite {
		t.Errorf("Write should return %q, got %q", fileOpWrite, op)
	}
}

func TestContextInfoStrippedFromDetail(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordContextFill(50000, 200000)
	cb.FinalizeTurn(time.Second)

	var b strings.Builder
	writeTurnDetail(&b, &cb.turns[0])
	detail := b.String()

	if strings.Contains(detail, "context:") {
		t.Error("context info should be stripped from turn detail")
	}
}

func TestUnhandledToolIncludesPayload(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordToolStart("web_search", `{"query":"golang context"}`)
	cb.RecordToolStart("ask_user", `{"question":"which branch?"}`)
	cb.FinalizeTurn(time.Second)

	var b strings.Builder
	writeTurnSummary(&b, &cb.turns[0])
	summary := b.String()

	// Unhandled tools must include their name AND payload.
	if !strings.Contains(summary, `web_search: {"query":"golang context"};`) {
		t.Errorf("unhandled tool should include payload, got: %s", summary)
	}
	if !strings.Contains(summary, `ask_user: {"question":"which branch?"};`) {
		t.Errorf("unhandled tool should include payload, got: %s", summary)
	}
}

func TestParseJSONFields(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		field  string
		want   string
		useRaw bool // if true, expect fallback to raw input
	}{
		{
			name:  "valid JSON",
			input: `{"file_path":"/src/main.go","old_string":"foo"}`,
			field: "file_path",
			want:  "/src/main.go",
		},
		{
			name:  "JSON with spaces",
			input: `{"file_path": "/src/main.go"}`,
			field: "file_path",
			want:  "/src/main.go",
		},
		{
			name:  "escaped quotes in value",
			input: `{"command":"echo \"hello\""}`,
			field: "command",
			want:  `echo "hello"`,
		},
		{
			name:  "truncated JSON repaired",
			input: `{"file_path":"/src/main.go","old_string":"some long conte`,
			field: "file_path",
			want:  "/src/main.go",
		},
		{
			name:   "completely invalid input",
			input:  "not json at all",
			field:  "file_path",
			useRaw: true,
		},
		{
			name:   "missing field falls back to raw",
			input:  `{"command":"go test"}`,
			field:  "file_path",
			useRaw: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONField(tt.input, tt.field)
			if tt.useRaw {
				if got != tt.input {
					t.Errorf("expected fallback to raw input, got %q", got)
				}
			} else if got != tt.want {
				t.Errorf("extractJSONField(%q, %q) = %q, want %q", tt.input, tt.field, got, tt.want)
			}
		})
	}
}

func TestExtractPathFromJSON(t *testing.T) {
	// file_path preferred over path.
	got := extractPathFromJSON(`{"file_path":"/a/b.go","path":"/c/d.go"}`)
	if got != "/a/b.go" {
		t.Errorf("expected file_path, got %q", got)
	}
	// Falls back to path.
	got = extractPathFromJSON(`{"path":"/c/d.go"}`)
	if got != "/c/d.go" {
		t.Errorf("expected path fallback, got %q", got)
	}
	// Falls back to raw input.
	got = extractPathFromJSON("not json")
	if got != "not json" {
		t.Errorf("expected raw fallback, got %q", got)
	}
}

func TestBufferInvocationScoped(t *testing.T) {
	// Simulates two supervisor invocations. Each should get a fresh buffer.
	// Since buffer is now a local in runWithRecovery, we verify the behavior
	// by checking that NewContextBuffer creates independent buffers.
	buf1 := NewContextBuffer("task A")
	buf1.StartTurn()
	buf1.RecordToolStart("Read", `{"file_path":"a.go"}`)
	buf1.AppendResult("result A")
	buf1.FinalizeTurn(time.Second)

	buf2 := NewContextBuffer("task B")
	buf2.StartTurn()
	buf2.RecordToolStart("Edit", `{"file_path":"b.go"}`)
	buf2.AppendResult("result B")
	buf2.FinalizeTurn(time.Second)

	// Each buffer should only contain its own data.
	if len(buf1.turns) != 1 || buf1.turns[0].ResultText != "result A" {
		t.Error("buf1 should only contain task A data")
	}
	if len(buf2.turns) != 1 || buf2.turns[0].ResultText != "result B" {
		t.Error("buf2 should only contain task B data")
	}

	// Continuation prompt should reference the correct original task.
	buf1.IncrementGeneration()
	prompt := buf1.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<mission>task A</mission>") {
		t.Error("buf1 continuation should reference task A")
	}
	if strings.Contains(prompt, "task B") {
		t.Error("buf1 should not contain task B data")
	}
}

func TestRepoStateClearedOnFailure(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.FinalizeTurn(time.Second)

	// Simulate first replacement: repo state captured successfully.
	cb.SetRepoState("M  src/main.go")
	cb.IncrementGeneration()
	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<repo_state>") {
		t.Error("expected repo_state after successful capture")
	}

	// Simulate second replacement: git fails, empty string clears stale state.
	cb.SetRepoState("") // captureRepoState returns "" on error
	cb.IncrementGeneration()
	prompt = cb.BuildContinuationPrompt()
	if strings.Contains(prompt, "<repo_state>") {
		t.Error("stale repo_state should be cleared when capture fails")
	}
}

func TestCaptureRepoState(t *testing.T) {
	// Test against the actual crucible repo (we're running inside it).
	ctx := context.Background()
	state, err := captureRepoState(ctx, ".")
	// Should succeed since we're in a git repo.
	if err != nil {
		t.Skipf("not in a git repo or git unavailable: %v", err)
	}
	if state == "" {
		t.Skip("no git changes to report")
	}
	// Should contain porcelain output (we have unstaged changes during test).
	if len(state) > maxRepoStateLen {
		t.Errorf("state exceeds max length: %d > %d", len(state), maxRepoStateLen)
	}
}

func TestCaptureRepoStateInvalidDir(t *testing.T) {
	ctx := context.Background()
	state, err := captureRepoState(ctx, "/nonexistent/path/unlikely/to/exist")
	if err == nil && state != "" {
		t.Error("expected error or empty state for invalid directory")
	}
}

func TestMultiGenerationBufferLifecycle(t *testing.T) {
	cb := NewContextBuffer("implement feature")

	// Generation 0: original work.
	cb.StartTurn()
	cb.RecordToolStart("Read", `{"file_path":"main.go"}`)
	cb.RecordToolStart("Edit", `{"file_path":"main.go"}`)
	cb.AppendResult("edited main.go")
	cb.RecordContextFill(170000, 200000)
	cb.FinalizeTurn(5 * time.Second)

	// Context exhausted — replacement.
	cb.SetRepoState("M  main.go")
	cb.IncrementGeneration()
	prompt1 := cb.BuildContinuationPrompt()

	// Generation 1 should carry forward gen 0 data.
	if strings.Contains(prompt1, "<generation>") {
		t.Error("generation tag should be stripped")
	}
	if !strings.Contains(prompt1, "M  main.go") {
		t.Error("missing repo state in gen 1 prompt")
	}

	// Generation 1 work.
	cb.StartTurn()
	cb.RecordToolStart("Bash", `{"command":"go test ./..."}`)
	cb.RecordThinking("tests should pass now")
	cb.AppendResult("all tests pass")
	cb.FinalizeTurn(3 * time.Second)

	// Exhausted again — second replacement.
	cb.SetRepoState("") // git fails this time
	cb.IncrementGeneration()
	prompt2 := cb.BuildContinuationPrompt()

	if strings.Contains(prompt2, "<generation>") {
		t.Error("generation tag should be stripped")
	}
	// Stale repo state should be cleared.
	if strings.Contains(prompt2, "<repo_state>") {
		t.Error("stale repo_state not cleared in gen 2")
	}
	// Thinking from gen 1 should be present.
	if !strings.Contains(prompt2, "[thinking] tests should pass now") {
		t.Error("lost thinking from gen 1")
	}
}

// --- TodoWrite progress table and result truncation removal tests ---

func TestTodoWriteProgressExtraction(t *testing.T) {
	cb := NewContextBuffer("implement feature")
	cb.StartTurn()
	cb.RecordToolStart("TodoWrite", `{"todos":[{"content":"Read files","status":"completed"},{"content":"Write code","status":"in_progress"}]}`)
	cb.RecordToolStart("Edit", `{"file_path":"main.go"}`)
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<task_progress>") {
		t.Error("missing task_progress section")
	}
	if !strings.Contains(prompt, "[x] Read files") {
		t.Error("task_progress should render completed items with [x]")
	}
	if !strings.Contains(prompt, "[~] Write code") {
		t.Error("task_progress should render in_progress items with [~]")
	}
}

func TestTodoWriteLastWinsWithinTurn(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	cb.RecordToolStart("TodoWrite", `{"todos":[{"content":"Step 1","status":"in_progress"}]}`)
	cb.RecordToolStart("Edit", `{"file_path":"a.go"}`)
	cb.RecordToolStart("TodoWrite", `{"todos":[{"content":"Step 1","status":"completed"},{"content":"Step 2","status":"in_progress"}]}`)
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<task_progress>") {
		t.Error("missing task_progress section")
	}
	// The second TodoWrite should win (last-write-wins within a turn).
	if !strings.Contains(prompt, "[x] Step 1") {
		t.Error("expected Step 1 as completed from second snapshot")
	}
	if !strings.Contains(prompt, "[~] Step 2") {
		t.Error("expected Step 2 as in_progress from second snapshot")
	}
}

func TestTodoWriteInSummarizedTurnStillRendered(t *testing.T) {
	cb := NewContextBuffer("big task")

	// Turn 0: has a TodoWrite snapshot (will end up in summary band for >15 turns).
	cb.StartTurn()
	cb.RecordToolStart("TodoWrite", `{"todos":[{"content":"Early task","status":"completed"}]}`)
	cb.FinalizeTurn(time.Second)

	// Fill enough turns so turn 0 is in the summary band (>15 turns needed).
	for i := 1; i < 20; i++ {
		cb.StartTurn()
		cb.RecordToolStart("Read", `{"file_path":"file.go"}`)
		cb.FinalizeTurn(time.Second)
	}
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	// writeTaskProgress scans ALL turns, not just recent — so it should
	// still find the snapshot from turn 0 even though it's in the summary band.
	if !strings.Contains(prompt, "<task_progress>") {
		t.Error("task_progress should render even when snapshot is in summarized turn")
	}
	if !strings.Contains(prompt, "[x] Early task") {
		t.Error("expected early TodoWrite snapshot to be found and formatted")
	}
}

func TestLargeResultTextNotTruncated(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	// A result well over the old 500-char limit.
	longResult := strings.Repeat("x", 2000)
	cb.AppendResult(longResult)
	cb.FinalizeTurn(time.Second)

	// Finalize a second turn so the first becomes "old".
	cb.StartTurn()
	cb.AppendResult("second")
	cb.FinalizeTurn(time.Second)

	// The first turn's ResultText should NOT be trimmed.
	if cb.turns[0].ResultText != longResult {
		t.Errorf("expected full result (%d chars), got %d chars", len(longResult), len(cb.turns[0].ResultText))
	}

	// Also verify it renders untruncated in the continuation prompt.
	cb.IncrementGeneration()
	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, longResult) {
		t.Error("large result text should appear untruncated in continuation prompt")
	}
}

func TestTodoWriteMalformedJSON(t *testing.T) {
	cb := NewContextBuffer("task")
	cb.StartTurn()
	// Malformed JSON — should still be captured and rendered verbatim as fallback.
	cb.RecordToolStart("TodoWrite", `{not valid json`)
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<task_progress>") {
		t.Error("task_progress should render even with malformed JSON")
	}
	if !strings.Contains(prompt, "{not valid json") {
		t.Error("malformed JSON should fall back to verbatim rendering")
	}
}

func TestBuildContinuationPromptMissionFirst(t *testing.T) {
	cb := NewContextBuffer("implement the widget")
	for range 20 {
		cb.StartTurn()
		cb.RecordToolStart("Read", `{"file_path":"file.go"}`)
		cb.AppendResult("did something")
		cb.FinalizeTurn(time.Second)
	}
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	missionIdx := strings.Index(prompt, "<mission>")
	summaryIdx := strings.Index(prompt, "<previous_work_summary>")
	if missionIdx < 0 {
		t.Fatal("missing <mission> tag")
	}
	if summaryIdx < 0 {
		t.Fatal("missing <previous_work_summary> tag")
	}
	if missionIdx >= summaryIdx {
		t.Errorf("<mission> (idx %d) must appear before <previous_work_summary> (idx %d)",
			missionIdx, summaryIdx)
	}
}

func TestBuildContinuationPromptReplacementNotice(t *testing.T) {
	cb := NewContextBuffer("build the feature")
	cb.StartTurn()
	cb.AppendResult("partial work")
	cb.FinalizeTurn(time.Second)
	cb.IncrementGeneration()

	prompt := cb.BuildContinuationPrompt()
	if !strings.Contains(prompt, "<replacement_notice>") {
		t.Error("missing <replacement_notice> tag")
	}
	if !strings.Contains(prompt, "Re-read any plan files") {
		t.Error("replacement notice should instruct re-reading plan files")
	}
}

// errors is needed for errTest.
var _ = errors.New
