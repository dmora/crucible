package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmora/agentrun"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Mock Process (streaming backend — no SequentialSender marker)
// ---------------------------------------------------------------------------

type mockProcess struct {
	mu      sync.Mutex
	output  chan agentrun.Message
	sends   []string
	stopped atomic.Bool
	termErr error
}

func newMockProcess() *mockProcess {
	return &mockProcess{output: make(chan agentrun.Message, 100)}
}

func (m *mockProcess) Output() <-chan agentrun.Message { return m.output }

func (m *mockProcess) Send(_ context.Context, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, message)
	return nil
}

func (m *mockProcess) Stop(_ context.Context) error {
	m.stopped.Store(true)
	close(m.output)
	return nil
}

func (m *mockProcess) Wait() error { return m.termErr }
func (m *mockProcess) Err() error  { return m.termErr }

func (m *mockProcess) Sends() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.sends))
	copy(cp, m.sends)
	return cp
}

func (m *mockProcess) emit(msg agentrun.Message) { m.output <- msg }

func (m *mockProcess) emitResult(content string, sr agentrun.StopReason) {
	m.output <- agentrun.Message{Type: agentrun.MessageResult, Content: content, StopReason: sr}
}

func (m *mockProcess) emitResultWithContext(content string, sr agentrun.StopReason, used, size int) {
	m.output <- agentrun.Message{
		Type: agentrun.MessageResult, Content: content, StopReason: sr,
		Usage: &agentrun.Usage{ContextUsedTokens: used, ContextSizeTokens: size},
	}
}

// ---------------------------------------------------------------------------
// Mock Sequential (spawn-per-turn) Process
// ---------------------------------------------------------------------------

type mockSequentialProcess struct{ *mockProcess }

func newMockSequentialProcess() *mockSequentialProcess {
	return &mockSequentialProcess{mockProcess: newMockProcess()}
}

func (m *mockSequentialProcess) SequentialSend() {}

var _ agentrun.SequentialSender = (*mockSequentialProcess)(nil)

// ---------------------------------------------------------------------------
// Mock Engine — returns pre-loaded processes in order
// ---------------------------------------------------------------------------

type mockEngine struct {
	mu        sync.Mutex
	processes []agentrun.Process
	startIdx  int
	starts    []agentrun.Session
}

func (e *mockEngine) Start(_ context.Context, sess agentrun.Session, _ ...agentrun.Option) (agentrun.Process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts = append(e.starts, sess)
	if e.startIdx < len(e.processes) {
		proc := e.processes[e.startIdx]
		e.startIdx++
		return proc, nil
	}
	return nil, fmt.Errorf("no more mock processes")
}

func (e *mockEngine) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Mock tool.Context (minimal implementation for runWithRecovery)
// ---------------------------------------------------------------------------

type mockState struct {
	mu   sync.Mutex
	data map[string]any
}

func newMockState() *mockState {
	return &mockState{data: make(map[string]any)}
}

func (s *mockState) Get(key string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *mockState) Set(key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *mockState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

type mockToolContext struct {
	context.Context
	state *mockState
}

func newMockToolContext() *mockToolContext {
	return &mockToolContext{Context: context.Background(), state: newMockState()}
}

func (m *mockToolContext) State() session.State                 { return m.state }
func (m *mockToolContext) ReadonlyState() session.ReadonlyState { return m.state }
func (m *mockToolContext) Artifacts() adkagent.Artifacts        { return nil }
func (m *mockToolContext) UserContent() *genai.Content          { return nil }
func (m *mockToolContext) InvocationID() string                 { return "inv-test" }
func (m *mockToolContext) AgentName() string                    { return "test-agent" }
func (m *mockToolContext) UserID() string                       { return "user-test" }
func (m *mockToolContext) AppName() string                      { return "crucible-test" }
func (m *mockToolContext) SessionID() string                    { return "session-test" }
func (m *mockToolContext) Branch() string                       { return "" }
func (m *mockToolContext) FunctionCallID() string               { return "fc-test" }
func (m *mockToolContext) Actions() *session.EventActions       { return &session.EventActions{} }
func (m *mockToolContext) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (m *mockToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
func (m *mockToolContext) RequestConfirmation(_ string, _ any) error            { return nil }

var _ tool.Context = (*mockToolContext)(nil)

// ---------------------------------------------------------------------------
// Helper: build a test processManager
// ---------------------------------------------------------------------------

func newTestPM(engine agentrun.Engine) *processManager {
	pm := &processManager{
		processes: make(map[string]agentrun.Process),
		engine:    engine,
		station:   "test-station",
		cwd:       ".",
		backend:   "claude",
	}
	pm.task = &TaskBuilder{station: "test-station", backend: "claude"}
	pm.observer = &StationObserver{station: "test-station"}
	pm.recovery = &RecoveryController{
		station:            "test-station",
		cwd:                ".",
		captureRepoStateFn: captureRepoState,
	}
	pm.persist = &StatePersister{station: "test-station"}
	return pm
}

// ---------------------------------------------------------------------------
// Test: streaming backends use RunTurn (Send) for BOTH gen 0 and gen > 0
// ---------------------------------------------------------------------------

func TestRunWithRecovery_StreamingUsesRunTurnOnReplacement(t *testing.T) {
	// Set up two streaming mock processes:
	// proc0: gen 0, will exhaust context → triggers replacement
	// proc1: gen 1 (replacement), should receive continuation prompt via Send()
	proc0 := newMockProcess()
	proc1 := newMockProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc0, proc1}}
	pm := newTestPM(engine)

	tctx := newMockToolContext()
	sessionID := "sess-streaming"
	task := "implement feature X"

	// proc0: emits work, then exhausts context (high fill ratio).
	go func() {
		// Wait for RunTurn's Send() to arrive.
		time.Sleep(20 * time.Millisecond)
		proc0.emit(agentrun.Message{Type: agentrun.MessageText, Content: "partial work"})
		proc0.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()

	// proc1: completes successfully after replacement.
	go func() {
		// Wait for proc0 to finish + replacement to start + RunTurn's Send().
		time.Sleep(200 * time.Millisecond)
		proc1.emitResult("final result", agentrun.StopEndTurn)
	}()

	var result strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, sessionID, stationInput{Task: task}, pm.task, "", &result,
		func(_ agentrun.Message) error { return nil },
		nil, nil,
	)
	if err != nil {
		t.Fatalf("runWithRecovery error: %v", err)
	}

	// Verify gen 0: Send() was called (streaming backend, RunTurn path).
	sends0 := proc0.Sends()
	if len(sends0) != 1 {
		t.Fatalf("gen 0: expected 1 Send() call (RunTurn), got %d", len(sends0))
	}
	if !strings.Contains(sends0[0], "implement feature X") {
		t.Errorf("gen 0: Send() should contain original task, got %q", sends0[0])
	}

	// Verify gen 1 start: empty initialPrompt for streaming gen > 0.
	engine.mu.Lock()
	if engine.starts[1].Prompt != "" {
		t.Errorf("gen 1: streaming replacement should have empty initialPrompt, got %q", engine.starts[1].Prompt)
	}
	engine.mu.Unlock()

	// Verify gen 1 (replacement): Send() was also called — this is the bug fix.
	// Before the fix, streaming replacements used drainFirstTurn (no Send),
	// so the continuation prompt was never delivered.
	sends1 := proc1.Sends()
	if len(sends1) != 1 {
		t.Fatalf("gen 1 (replacement): expected 1 Send() call (RunTurn), got %d — "+
			"this means the drain-only bug is still present", len(sends1))
	}
	if !strings.Contains(sends1[0], "<continuation_context>") {
		t.Errorf("gen 1: Send() should contain continuation prompt, got %q", sends1[0])
	}
	if !strings.Contains(sends1[0], "<mission>") {
		t.Errorf("gen 1: Send() should contain <mission> tag, got %q", sends1[0])
	}

	// Final result should be from proc1.
	if result.String() != "final result" {
		t.Errorf("expected 'final result', got %q", result.String())
	}
}

// ---------------------------------------------------------------------------
// Test: spawn-per-turn backends use drainFirstTurn (no Send) on first turn
// ---------------------------------------------------------------------------

func TestRunWithRecovery_SpawnPerTurnUsesDrain(t *testing.T) {
	proc := newMockSequentialProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc}}
	pm := newTestPM(engine)
	pm.backend = "codex"

	tctx := newMockToolContext()

	go func() {
		time.Sleep(20 * time.Millisecond)
		proc.emitResult("codex result", agentrun.StopEndTurn)
	}()

	var result strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, "sess-spawn", stationInput{Task: "task"}, pm.task, "",
		&result, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("runWithRecovery error: %v", err)
	}

	// Spawn-per-turn: prompt is in CLI args, so Send should NOT be called.
	sends := proc.Sends()
	if len(sends) != 0 {
		t.Errorf("spawn-per-turn: expected 0 Send() calls (drain path), got %d", len(sends))
	}
}

// ---------------------------------------------------------------------------
// Test: buffer is invocation-scoped (fresh per runWithRecovery call)
// ---------------------------------------------------------------------------

func TestRunWithRecovery_BufferInvocationScoped(t *testing.T) {
	// Two sequential runWithRecovery calls on the same processManager should
	// NOT share buffer state. We verify by checking continuation prompts
	// don't leak across invocations.

	// Invocation 1: exhausts → replacement → completes.
	proc1a := newMockProcess()
	proc1b := newMockProcess()
	// Invocation 2: completes on first try.
	proc2 := newMockProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc1a, proc1b, proc2}}
	pm := newTestPM(engine)

	tctx := newMockToolContext()

	// --- Invocation 1 ---
	go func() {
		time.Sleep(20 * time.Millisecond)
		proc1a.emit(agentrun.Message{
			Type: agentrun.MessageText,
			Tool: &agentrun.ToolCall{Name: "Edit", Input: []byte(`{"file_path":"alpha.go"}`)},
		})
		proc1a.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc1b.emitResult("invocation 1 done", agentrun.StopEndTurn)
	}()

	var result1 strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, "sess-scope", stationInput{Task: "task alpha"}, pm.task, "",
		&result1, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("invocation 1 error: %v", err)
	}

	// The replacement (proc1b) should have received a continuation mentioning alpha.
	sends1b := proc1b.Sends()
	if len(sends1b) != 1 {
		t.Fatalf("invocation 1 replacement: expected 1 Send, got %d", len(sends1b))
	}
	if !strings.Contains(sends1b[0], "alpha") {
		t.Errorf("invocation 1 continuation should mention alpha, got %q", sends1b[0])
	}

	// --- Invocation 2 (same pm, same sessionID) ---
	// Must kill the old process from pm.processes first since proc1b is still there.
	pm.mu.Lock()
	delete(pm.processes, "sess-scope")
	pm.mu.Unlock()

	go func() {
		time.Sleep(20 * time.Millisecond)
		proc2.emitResult("invocation 2 done", agentrun.StopEndTurn)
	}()

	var result2 strings.Builder
	_, _, err = pm.recovery.Run(
		context.Background(), tctx, pm, "sess-scope", stationInput{Task: "task beta"}, pm.task, "",
		&result2, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("invocation 2 error: %v", err)
	}

	// Invocation 2 should NOT have received anything about alpha.
	sends2 := proc2.Sends()
	if len(sends2) != 1 {
		t.Fatalf("invocation 2: expected 1 Send, got %d", len(sends2))
	}
	if strings.Contains(sends2[0], "alpha") || strings.Contains(sends2[0], "<continuation_context>") {
		t.Errorf("invocation 2 should NOT carry invocation 1's buffer data, got %q", sends2[0])
	}
	if !strings.Contains(sends2[0], "beta") {
		t.Errorf("invocation 2 Send should contain 'beta', got %q", sends2[0])
	}
}

// ---------------------------------------------------------------------------
// Test: captureRepoState overwrites correctly during replacement loop
// ---------------------------------------------------------------------------

func TestRunWithRecovery_RepoStateOverwriteInLoop(t *testing.T) {
	// 3-generation run: gen0 exhausts, gen1 exhausts, gen2 completes.
	// Each replacement captures fresh repo state via captureRepoStateFn.
	// We inject deterministic state per generation and verify it appears
	// in the correct continuation prompt without leaking across generations.
	proc0 := newMockProcess()
	proc1 := newMockProcess()
	proc2 := newMockProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc0, proc1, proc2}}
	pm := newTestPM(engine)

	// Inject deterministic repo state: each call returns a unique string.
	// gen0→gen1 replacement captures "STATE_GEN1", gen1→gen2 captures "".
	var captureCalls atomic.Int32
	pm.recovery.captureRepoStateFn = func(_ context.Context, _ string) (string, error) {
		call := captureCalls.Add(1)
		switch call {
		case 1:
			return "M  alpha.go\n M  beta.go\nSTATE_GEN1_MARKER", nil
		case 2:
			// Simulate git failure on second replacement — should clear stale state.
			return "", fmt.Errorf("git not available")
		default:
			return "UNEXPECTED_CALL", nil
		}
	}

	tctx := newMockToolContext()

	go func() {
		time.Sleep(20 * time.Millisecond)
		proc0.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc1.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()
	go func() {
		time.Sleep(400 * time.Millisecond)
		proc2.emitResult("final", agentrun.StopEndTurn)
	}()

	var result strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, "sess-repo", stationInput{Task: "task"}, pm.task, "",
		&result, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("runWithRecovery error: %v", err)
	}

	// Verify all 3 processes were started (gen 0, 1, 2).
	engine.mu.Lock()
	numStarts := len(engine.starts)
	engine.mu.Unlock()
	if numStarts != 3 {
		t.Fatalf("expected 3 engine.Start() calls, got %d", numStarts)
	}

	// Verify captureRepoStateFn was called exactly twice (once per replacement).
	if got := captureCalls.Load(); got != 2 {
		t.Fatalf("expected 2 captureRepoStateFn calls, got %d", got)
	}

	sends1 := proc1.Sends()
	sends2 := proc2.Sends()
	if len(sends1) != 1 {
		t.Fatalf("gen 1: expected 1 Send, got %d", len(sends1))
	}
	if len(sends2) != 1 {
		t.Fatalf("gen 2: expected 1 Send, got %d", len(sends2))
	}

	// Gen 1's continuation prompt should contain the repo state from capture call 1.
	if !strings.Contains(sends1[0], "<repo_state>") {
		t.Error("gen 1 prompt should contain <repo_state> block")
	}
	if !strings.Contains(sends1[0], "STATE_GEN1_MARKER") {
		t.Error("gen 1 prompt should contain STATE_GEN1_MARKER from first capture")
	}
	if !strings.Contains(sends1[0], "alpha.go") {
		t.Error("gen 1 prompt should contain alpha.go from first capture")
	}

	// Gen 2's continuation prompt should NOT contain repo_state (capture failed,
	// SetRepoState("") should have cleared the stale gen 1 state).
	if strings.Contains(sends2[0], "<repo_state>") {
		t.Error("gen 2 prompt should NOT contain <repo_state> (capture failed, state cleared)")
	}
	if strings.Contains(sends2[0], "STATE_GEN1_MARKER") {
		t.Error("gen 2 prompt must NOT leak gen 1's repo state — stale state not cleared")
	}
	if strings.Contains(sends2[0], "alpha.go") {
		t.Error("gen 2 prompt must NOT contain alpha.go from gen 1's stale state")
	}

	// Generation tag is stripped from operator-facing continuation prompt.
	if strings.Contains(sends2[0], "<generation>") {
		t.Error("generation tag should be stripped from continuation prompt")
	}
}

// ---------------------------------------------------------------------------
// Test: skill wrapping on replacement — continuation prompt gets skill prefix
// ---------------------------------------------------------------------------

func TestRunWithRecovery_SkillWrappingOnReplacement(t *testing.T) {
	proc0 := newMockProcess()
	proc1 := newMockProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc0, proc1}}
	pm := newTestPM(engine)
	pm.task = &TaskBuilder{station: "build", skill: "feature-dev:feature-dev", backend: "claude"}

	tctx := newMockToolContext()

	go func() {
		time.Sleep(20 * time.Millisecond)
		proc0.emit(agentrun.Message{Type: agentrun.MessageText, Content: "partial"})
		proc0.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc1.emitResult("done", agentrun.StopEndTurn)
	}()

	var result strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, "sess-skill", stationInput{Task: "implement X"}, pm.task, "",
		&result, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	sends1 := proc1.Sends()
	if len(sends1) != 1 {
		t.Fatalf("expected 1 Send, got %d", len(sends1))
	}
	if !strings.HasPrefix(sends1[0], "Load your feature-dev:feature-dev skill and then:") {
		t.Errorf("replacement Send should start with skill prefix, got %q", sends1[0][:min(80, len(sends1[0]))])
	}
	if !strings.Contains(sends1[0], "<continuation_context>") {
		t.Error("replacement Send should contain <continuation_context>")
	}
}

// ---------------------------------------------------------------------------
// Test: spawn-per-turn replacement — initialPrompt carries full prompt
// ---------------------------------------------------------------------------

func TestRunWithRecovery_SpawnPerTurnReplacement(t *testing.T) {
	// proc0: streaming gen 0, exhausts context → triggers replacement.
	// proc1: spawn-per-turn gen 1, completes — prompt must be in Start().
	proc0 := newMockProcess()
	proc1 := newMockSequentialProcess()

	engine := &mockEngine{processes: []agentrun.Process{proc0, proc1}}
	pm := newTestPM(engine)
	pm.task = &TaskBuilder{station: "build", skill: "feature-dev:feature-dev", backend: "codex"}

	tctx := newMockToolContext()

	go func() {
		time.Sleep(20 * time.Millisecond)
		proc0.emit(agentrun.Message{Type: agentrun.MessageText, Content: "partial"})
		proc0.emitResultWithContext("", agentrun.StopMaxTokens, 190000, 200000)
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		proc1.emitResult("codex done", agentrun.StopEndTurn)
	}()

	var result strings.Builder
	_, _, err := pm.recovery.Run(
		context.Background(), tctx, pm, "sess-spt-replace", stationInput{Task: "implement Y"}, pm.task, "",
		&result, func(_ agentrun.Message) error { return nil }, nil, nil,
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Spawn-per-turn gen > 0: initialPrompt must be non-empty (baked into Start).
	engine.mu.Lock()
	startPrompt := engine.starts[1].Prompt
	engine.mu.Unlock()
	if startPrompt == "" {
		t.Fatal("spawn-per-turn replacement: initialPrompt must be non-empty")
	}
	if !strings.Contains(startPrompt, "feature-dev:feature-dev") {
		t.Error("spawn-per-turn replacement: initialPrompt should contain skill prefix")
	}
	if !strings.Contains(startPrompt, "<continuation_context>") {
		t.Error("spawn-per-turn replacement: initialPrompt should contain <continuation_context>")
	}

	// Spawn-per-turn drains, never sends.
	sends1 := proc1.Sends()
	if len(sends1) != 0 {
		t.Errorf("spawn-per-turn: expected 0 Send() calls, got %d", len(sends1))
	}
}
