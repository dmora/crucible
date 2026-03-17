# S6: Pseudo Context Window — Sliding Window + LLM Compaction

## Context

### Problem

S1 (deterministic buffer, #61) records station activity into a `ContextBuffer` and constructs continuation prompts using fixed temporal bands. This has two fundamental flaws:

1. **Lossy recording** — S1 truncates aggressively at capture time: tool inputs to 100 chars, tool outputs to 200 chars, thinking to 500 chars. This destroys information before any summarization can happen. The buffer records a sketch of what happened, not a timeline.

2. **Mechanical summarization** — The temporal band system (`writeTurnSummary`, `writeBandDetail`, `writeBandRecent`) produces formulaic output. Older turns compress to `Turn 0: Read main.go; Edited config.yaml; ERRORS: 1;`. This captures *what tools ran* but loses *why decisions were made*, *what the station was thinking*, and *how partial progress connects to the remaining work*.

The result: replacement stations start with a grocery list of past actions instead of a coherent understanding of the work so far.

### What S6 Is

S6 is a **pseudo context window** — a sliding window over the full-fidelity event timeline that uses LLM compaction to produce high-quality continuation context for replacement stations.

The model follows ADK's compaction architecture:

```
BEFORE COMPACTION (Raw Event Timeline)
┌─────────────────────────────────────┬──────────────────────┐
│  Older Events (T1 ... T15)          │  Recent Events       │
│  Candidates for Compaction          │  (T16 ... T20)       │
│                                     │  Retained Raw        │
└─────────────────────────────────────┴──────────────────────┘

COMPACTION TRIGGER
  Token threshold exceeded → split timeline → summarize older events

AFTER COMPACTION (Context for Replacement Station)
┌──────────────────────────────┬──────────────────────────────┐
│  Summary Event (T1-T15)      │  Raw Events (T16...T20)      │
│  [LLM-Compacted Content]     │  [Full Fidelity]             │
└──────────────────────────────┴──────────────────────────────┘
```

The replacement station receives a **composite timeline**: one LLM-summarized block covering compacted older events, followed by raw recent events at full fidelity.

### Design Principle: Composability and Isolated Testing

The previous design revision mixed window splitting, summarization calling, and context construction directly into `ContextBuffer` and `processManager`. This made the components impossible to test without either real LLM calls or injected function closures threaded through multiple layers.

S6's architecture decomposes into **four independently testable components** with no cross-dependencies:

```
EventStore ──→ WindowSplitter ──→ EventSerializer ──→ EventSummarizer
                                                           │
                                         ContextBuilder ←──┘
                                               │
                                        Continuation Prompt
```

Each component is a pure function or a narrow interface. Testing requires zero infrastructure — just structs and strings.

### Current S1 Implementation

**ContextBuffer** (`internal/agent/context_buffer.go`) — monolithic event recorder + serializer + prompt builder:
- `TurnRecord` per RunTurn: tool calls (name, input, output), file ops, errors, result text, thinking, TodoWrite snapshot, context fill metrics
- **Aggressive truncation at capture**: `maxInputSummaryLen=100`, `maxOutputSummaryLen=200`, `maxThinkingLen=500`
- `BuildContinuationPrompt()` — temporal bands + deterministic sections, all in one method
- Ring buffer: `maxBufferTurns=50`

**runWithRecovery** (`internal/agent/agentrun.go:487-652`) — recovery loop:
- Detects exhaustion, kills process, calls `buf.BuildContinuationPrompt()`, loops
- Up to 4 attempts (original + 3 replacements)

**buildWrappedHandler** (`internal/agent/agentrun.go:654-706`) — event bridge:
- Intercepts `agentrun.Message` events, records into `ContextBuffer`, delegates to UI handler

**runOneShot** (`internal/agent/agent_title.go:84-136`) — ephemeral ADK agent:
- Creates in-memory ADK session, runs one turn, returns `oneShotResult{Text, Usage}`
- Currently used for title generation (Flash, 40 max tokens, thinking disabled)
- Signature: `func runOneShot(ctx, model, agentName, instruction, prompt string, genCfg *genai.GenerateContentConfig) (oneShotResult, error)`

### Constraints

- **Full fidelity recording** — thinking blocks captured without token limits. Tool I/O caps raised substantially. Truncation deferred to compaction time (by the LLM).
- **Composite output** — continuation prompt = compacted older events + raw recent events. Not a uniform summary.
- **Isolated testability** — every component testable with zero LLM calls and zero infrastructure.
- **Latency** — compaction adds a model call. Must stay within ~15s total replacement time.
- **Reliability** — `runOneShot()` can fail. S1's deterministic `BuildContinuationPrompt()` is the fallback.
- **Cost** — Flash per replacement: ~$0.002-0.01. Acceptable at max 3 replacements.

### Context Manifest

Files examined:
- `internal/agent/context_buffer.go` (all, 536 lines) — S1 buffer type, TurnRecord, BufferedToolCall, truncation constants, temporal bands, BuildContinuationPrompt, all write*/extract* helpers
- `internal/agent/agentrun.go` (lines 487-706) — `runWithRecovery` loop, `buildWrappedHandler` event bridge
- `internal/agent/agent_title.go` (all, 162 lines) — `runOneShot()` signature and implementation, `generateTitle()`, model selection, `oneShotResult`
- `internal/agent/result.go` — `oneShotResult`, `UsageInfo` types
- `internal/agent/coordinator.go` — model construction, smallModel/largeModel resolution

---

## Solution Pool

All candidates share: **full-fidelity event recording** (remove truncation caps) and a **sliding window split** (older vs. recent). They differ on **component decomposition** and **how the pipeline is composed**.

### Candidate A: Four-Component Pipeline (Decomposed)

Decompose the compaction pipeline into four independent components, each with a single responsibility and testable in isolation:

1. **EventStore** (`ContextBuffer`) — records events, provides turn access. No serialization, no splitting, no summarization.
2. **WindowSplitter** — pure function: `(turns []TurnRecord, recentWindow int) → (older, recent []TurnRecord)`. No buffer knowledge.
3. **EventSerializer** — pure function: `(turns []TurnRecord) → string`. Renders turns to the chronological text format. No buffer knowledge, no LLM.
4. **EventSummarizer** — interface: `Summarize(ctx, serializedOlderEvents string) → (summary string, err error)`. Single method, wraps `runOneShot()`. Mockable.
5. **ContextBuilder** — pure function: `(summary string, serializedRecent string, deterministicSections DeterministicContext) → string`. Assembles the final prompt. No LLM, no buffer.

**Composition in `runWithRecovery`:**
```
older, recent := SplitWindow(buf.Turns(), 5)
serializedOlder := SerializeEvents(older)
serializedRecent := SerializeEvents(recent)
summary, err := summarizer.Summarize(ctx, serializedOlder)
if err != nil { fall back to S1 }
prompt := BuildContinuationContext(summary, serializedRecent, deterministicCtx)
```

**Strengths:**
- Each component tested with plain data — `[]TurnRecord` slices, plain strings, no mocks needed (except the Summarizer interface)
- WindowSplitter tested with synthetic turn slices: 3 turns, 20 turns, 50 turns, edge cases
- EventSerializer tested by constructing TurnRecords in-memory and asserting output format
- ContextBuilder tested with literal strings — no LLM, no buffer, no infrastructure
- EventSummarizer is the only component that touches the LLM — testable via a single-method interface mock
- Components are reusable: EventSerializer could serve debugging/logging, WindowSplitter could be used for future incremental compaction

**Weaknesses:**
- More files than the monolithic approach — 4-5 new files vs. methods on one type
- The composition site (`runWithRecovery`) must know all components and thread them together
- Slight verbosity at the integration level compared to `buf.BuildCompactedPrompt(compactFn)`

**Fit:**
- Maximizes testability and composability. Each component has exactly one reason to change. Adding a new serialization format, changing the window strategy, or swapping the summarizer model requires changing exactly one component.

### Candidate B: Method Decomposition on ContextBuffer (Internal Split)

Keep all functionality on `ContextBuffer` but decompose into well-defined internal methods. The public API is `BuildCompactedPrompt(ctx, summarizer)`, but internally it calls private methods that are individually testable via exported helpers or table-driven tests.

**How it works:**
- `ContextBuffer.olderTurns()`, `ContextBuffer.recentTurns()` — splitting
- `ContextBuffer.serializeTurns(turns)` — serialization
- `ContextBuffer.BuildCompactedPrompt(ctx, summarizer)` — orchestration
- Summarizer is still an interface, injected

**Strengths:**
- Fewer files — everything stays in `context_buffer.go`
- Single type owns the entire lifecycle (recording → splitting → serialization → assembly)
- Simpler composition in `runWithRecovery`: `buf.BuildCompactedPrompt(ctx, summarizer)`

**Weaknesses:**
- Methods on `ContextBuffer` are harder to test in isolation because they access `cb.turns`, `cb.repoState`, `cb.originalTask` — the test must always construct a full `ContextBuffer` with the right internal state
- Splitting logic can't be tested without recording events first
- Serialization can't be tested without a populated buffer
- `ContextBuffer` accumulates responsibilities: recording + splitting + serialization + assembly. Single Responsibility Principle violation.
- Can't reuse serialization or splitting outside the buffer context

**Fit:**
- Simpler file structure but worse testability. The user explicitly asked for isolated testing with zero dependencies. Methods on a stateful type can't deliver that — they always require populating the type first.

### Candidate C: Functional Pipeline with Type Aliases (Middle Ground)

Export the pipeline stages as package-level functions that operate on `[]TurnRecord` and strings, but don't introduce new types or interfaces for the splitter/serializer/builder. The Summarizer is still an interface. Components are functions, not types.

**How it works:**
- `SplitWindow(turns []TurnRecord, recentN int) (older, recent []TurnRecord)` — exported function
- `SerializeEvents(turns []TurnRecord) string` — exported function
- `BuildContinuationContext(summary, serializedRecent string, ctx DeterministicContext) string` — exported function
- `EventSummarizer` interface with `Summarize(ctx, text) (string, error)`
- `ContextBuffer` stays as the event store (recording only)

**Strengths:**
- All pipeline stages are exported pure functions — testable with synthetic data, no mocks
- No new types beyond `EventSummarizer` interface and `DeterministicContext` struct
- Functions live alongside `ContextBuffer` in the same package — no file proliferation
- Composition at call site is explicit: each function call is visible in `runWithRecovery`

**Weaknesses:**
- Package-level functions are discoverable but don't communicate the pipeline structure as clearly as types
- No grouping mechanism beyond file placement — a future reader must understand the pipeline from the call site

**Fit:**
- Best balance of composability and simplicity. Functions are the lightest-weight unit of composition in Go. The pipeline is explicit at the call site. Testing is straightforward.

### Self-Critique Matrix

| Candidate | Strongest Counter-Argument | Worst-Case | Hidden Cost |
|-----------|---------------------------|------------|-------------|
| **A: Four-Component Pipeline** | More files and types than needed for a pipeline that has exactly one composition site | The 4-5 new files add navigation overhead; a future reader must trace through multiple types to understand the flow | Interface for WindowSplitter and EventSerializer is overkill — there's exactly one implementation of each |
| **B: Methods on ContextBuffer** | Can't test splitting or serialization without first populating a ContextBuffer with recording calls | Testing a 20-turn split requires 20 `StartTurn`/`RecordToolStart`/`FinalizeTurn` call sequences in every test | Responsibility creep — ContextBuffer becomes a god object that records, splits, serializes, summarizes, and assembles |
| **C: Functional Pipeline** | Package-level functions don't communicate pipeline structure — must read the call site to understand flow | Functions aren't grouped into a discoverable unit; a new contributor may miss `SerializeEvents` when looking for serialization | Slightly less discoverable than types with methods, but Go's package-level function convention is well-understood |

---

## Decisions

### ADR-1: Functional Pipeline with EventSummarizer Interface (Candidate C)

In the context of **decomposing the compaction pipeline for isolated testability**, facing **the tension between type-heavy decomposition (Candidate A: 4 new types) and method-on-buffer monolith (Candidate B: untestable without populated buffer)**, we decided **to implement the pipeline as exported package-level pure functions (SplitWindow, SerializeEvents, BuildContinuationContext) with a single EventSummarizer interface for the LLM call**, and neglected **four-component types (unnecessary — only one composition site) and method decomposition on ContextBuffer (untestable in isolation, SRP violation)**, to achieve **zero-dependency isolated testing: each function testable with synthetic `[]TurnRecord` slices or plain strings, the summarizer testable via a single-method interface mock**, accepting **that the pipeline structure is implicit in the call site rather than expressed through types**.

Confidence: **high** — Go's package-level function convention is well-understood. Each function has exactly one input type and one output type. Testing is trivially obvious. The composition site in `runWithRecovery` is the single place these are wired together.

Grounding: The current S1 implementation (`context_buffer.go`) mixes recording, splitting, and serialization in one type. The `temporalBands()` function (line 265-277) is already a package-level pure function — the pattern exists. The proposed functions extend this pattern.

Rejected: Candidate A (types for each pipeline stage) — Go interfaces are justified when there are multiple implementations or when the interface enables test doubles. WindowSplitter and EventSerializer have exactly one implementation. Making them interfaces adds ceremony without benefit. Candidate B (methods on ContextBuffer) — the user explicitly requires isolated testing. Methods on a stateful type require populating that type's state first, which means every serialization test also tests recording.

### ADR-2: Full-Fidelity Event Recording (Unchanged from v2)

In the context of **building the pseudo context window**, facing **S1's aggressive truncation destroying information before summarization**, we decided **to remove token caps on thinking blocks and increase tool I/O capture limits substantially**, accepting **higher per-event memory usage within the invocation-scoped buffer**.

Confidence: **high** — buffer is invocation-scoped (GC'd on return).

New capture limits:

| Field | S1 Limit | S6 Limit | Rationale |
|-------|----------|----------|-----------|
| Thinking | 500 chars | **Unlimited** | Reasoning chain — most valuable context. No cap. |
| Tool input | 100 chars | 4K chars | Full command strings, edit descriptions, search patterns. |
| Tool output | 200 chars | 8K chars | Full error traces, test output, command results. |
| Turn result | Unlimited | Unlimited | No change. |
| Errors | Unlimited | Unlimited | No change. |
| Buffer turns | 50 | 50 | No change — ring buffer prevents unbounded growth. |

### ADR-3: Fixed Recent Window of 5 Turns (Unchanged from v2)

Split logic:
- **≤5 turns total**: no compaction — all turns are "recent," serialized raw. No LLM call.
- **>5 turns total**: compact turns 0...(N-5), pass turns (N-4)...N raw.

Confidence: **high**.

### ADR-4: S1 Fallback on Compaction Failure (Unchanged from v2)

Fall back to S1's deterministic `BuildContinuationPrompt()` on any `EventSummarizer.Summarize()` error. Zero reliability regression.

Confidence: **high**.

### ADR-5: Narrative Compaction Output (Unchanged from v2)

Flash produces free-form narrative (handoff briefing), not structured XML sections. The output format matches what the consuming LLM handles best.

Confidence: **medium** — prompt will need iterative tuning.

### ADR-6: ContextBuffer Becomes Pure Event Store

In the context of **decomposing responsibilities**, facing **ContextBuffer currently owning recording, splitting, serialization, and prompt assembly**, we decided **to strip ContextBuffer down to event recording only — it stores TurnRecords and provides read access, but does not serialize, split, or assemble prompts**, and neglected **keeping serialization on ContextBuffer (convenient but untestable in isolation) and creating a separate EventStore type (ContextBuffer already exists and is well-named)**, to achieve **single responsibility: ContextBuffer records events, nothing else**, accepting **that S1's `BuildContinuationPrompt()` must be preserved alongside the new pipeline for fallback, so ContextBuffer retains that method during the transition period**.

Confidence: **high** — ContextBuffer has a clear recording API (`StartTurn`, `RecordToolStart`, `RecordToolOutput`, `RecordThinking`, `RecordError`, `AppendResult`, `FinalizeTurn`). Removing the serialization/assembly methods doesn't break this API. The new pipeline functions consume `[]TurnRecord` directly.

Grounding: `ContextBuffer` currently has 15+ methods. 7 are recording methods (keep), 1 is `BuildContinuationPrompt` (keep as S1 fallback), and the rest are serialization/splitting helpers (move to pipeline functions).

---

## Component Specification

### Overview: Four Components, One Composition Site

```
┌─────────────────────────────────────────────────────────────┐
│                    runWithRecovery (composition site)         │
│                                                              │
│  turns := buf.Turns()                                        │
│  older, recent := SplitWindow(turns, recentWindow)           │
│  serializedOlder := SerializeEvents(older)                   │
│  serializedRecent := SerializeEvents(recent)                 │
│  summary, err := summarizer.Summarize(ctx, serializedOlder)  │
│  if err != nil { task = buf.BuildContinuationPrompt() }      │
│  else { task = BuildContinuationContext(...) }                │
│                                                              │
└─────────────────────────────────────────────────────────────┘
         │              │              │              │
         ▼              ▼              ▼              ▼
    EventStore    WindowSplitter  EventSerializer  ContextBuilder
   (ContextBuffer)  (pure func)    (pure func)     (pure func)
                                        │
                                   EventSummarizer
                                    (interface)
```

### Component 1: EventStore (ContextBuffer)

**Responsibility:** Record station events at full fidelity. Provide read access to the turn timeline.

**What changes from S1:**
- Remove truncation in `RecordThinking()` — store full content (currently capped at `maxThinkingLen=500`)
- Increase cap in `RecordToolStart()` — `maxInputSummaryLen` from 100 → 4096
- Increase cap in `RecordToolOutput()` — `maxOutputSummaryLen` from 200 → 8192
- Add `Turns() []TurnRecord` — returns a copy of the turns slice for pipeline consumption

**What stays the same:** `StartTurn`, `RecordError`, `AppendResult`, `RecordFileOp`, `RecordContextFill`, `FinalizeTurn`, `Generation`, `IncrementGeneration`, `SetRepoState`, `ShouldReplace`, `AtGenerationCap`, `BuildContinuationPrompt` (preserved as S1 fallback).

**What does NOT belong here:** Serialization, splitting, summarization, prompt assembly. These move to pipeline functions.

**Testing:** Record events via the existing API, assert `Turns()` returns full-fidelity data. A 50K-char thinking block round-trips without truncation. Tool inputs at 4K chars round-trip. Tool outputs at 8K chars round-trip.

### Component 2: WindowSplitter (Pure Function)

```go
func SplitWindow(turns []TurnRecord, recentWindow int) (older, recent []TurnRecord)
```

**Responsibility:** Partition a turn timeline into older events (compaction candidates) and recent events (retained raw).

**Behavior:**
- If `len(turns) <= recentWindow`: older is empty, recent is all turns. (No compaction needed.)
- If `len(turns) > recentWindow`: `older = turns[:len(turns)-recentWindow]`, `recent = turns[len(turns)-recentWindow:]`
- `recentWindow` defaults to 5 but is a parameter, not a constant — callers can tune it.
- Returns slices (not copies) for efficiency. Callers must not mutate.

**Inputs:** `[]TurnRecord` (from `buf.Turns()`), `recentWindow int`
**Outputs:** two `[]TurnRecord` slices
**Error conditions:** none — pure partitioning, always succeeds. Zero turns → both empty.

**Testing:** Table-driven tests with synthetic `[]TurnRecord` slices:
- 0 turns → ([], [])
- 3 turns, window 5 → ([], [T0, T1, T2])
- 5 turns, window 5 → ([], [T0, T1, T2, T3, T4])
- 6 turns, window 5 → ([T0], [T1, T2, T3, T4, T5])
- 20 turns, window 5 → ([T0...T14], [T15...T19])
- 50 turns, window 5 → ([T0...T44], [T45...T49])
- Window 0 → all turns are "older," no recent
- Window equal to len → all turns are "recent," no older

No buffer needed. No LLM. No infrastructure. Just struct slices.

### Component 3: EventSerializer (Pure Function)

```go
func SerializeEvents(turns []TurnRecord) string
```

**Responsibility:** Render a slice of TurnRecords to a chronological text format suitable for LLM consumption (both as compaction input and as raw recent events in the continuation prompt).

**Output format:**

```
=== Turn 0 (12.3s) ===
[thinking]
The user wants me to implement a retry mechanism. I should first check
the existing error handling in the HTTP client...
(full thinking block, no truncation)

[tool] Read {"file_path": "/src/http/client.go"}
  → (file contents, up to 8K chars)

[tool] Edit {"file_path": "/src/http/client.go", "old_string": "...", "new_string": "..."}
  → Applied edit successfully.

[error] Tests failed. The retry wrapper is not handling the mock server correctly.

[result]
I've added a retry wrapper to the HTTP client...

=== Turn 1 (45.2s) ===
...
```

**Format rules:**
- Turn header: `=== Turn {seq} ({elapsed}) ===`
- Thinking: `[thinking]\n{full content}\n` — no truncation, no cap
- Tool calls: `[tool] {name} {input}\n  → {output}\n` — input and output as stored (already capped at 4K/8K by EventStore)
- Errors: `[error] {text}\n`
- Result: `[result]\n{text}\n`
- Events within a turn appear in recording order: thinking first (if present), then tool calls interleaved with their outputs, then errors, then result
- Empty fields omitted (no `[thinking]` line if thinking is empty)

**Inputs:** `[]TurnRecord`
**Outputs:** string
**Error conditions:** none — deterministic transformation.

**Testing:** Construct TurnRecords in-memory (no buffer needed):
```go
turns := []TurnRecord{
    {Seq: 0, Elapsed: 12*time.Second, Thinking: "reasoning...",
     ToolCalls: []BufferedToolCall{{Name: "Read", InputSummary: `{"file_path":"main.go"}`, OutputSummary: "package main..."}},
     ResultText: "I read the file."},
}
got := SerializeEvents(turns)
// assert exact output format
```

No buffer. No LLM. No infrastructure. Just structs → string.

### Component 4: EventSummarizer (Interface)

```go
type EventSummarizer interface {
    Summarize(ctx context.Context, serializedOlderEvents string) (string, error)
}
```

**Responsibility:** Take serialized older events and produce a compacted narrative summary. This is the only component that touches the LLM.

**Production implementation:** `llmSummarizer` struct holding a `Model` reference. `Summarize()` calls `runOneShot()` with:
- System instruction: the compaction prompt (embedded template)
- User content: the serialized older events
- `GenerateContentConfig`: `MaxOutputTokens: 8192`, `Temperature: 0.1`, default thinking config (no artificial constraints)

**Compaction prompt (system instruction):**

Role: You are compressing a work session log into a handoff briefing for a replacement agent. The replacement agent will continue the same task but has no prior context — your summary is its entire understanding of what happened before it started.

Instructions:
- Write a chronological narrative of what was accomplished, what was attempted, and where work was left off.
- Preserve the reasoning chain: WHY decisions were made, not just WHAT tools were called. If the agent tried approach A, found it didn't work because of reason X, and switched to approach B — capture that causal sequence.
- Explicitly call out failed approaches and why they failed. The replacement agent must not repeat them.
- List all files that were created or modified, with a brief note on what changed.
- If the agent was in the middle of a multi-step plan, describe where it was in that plan.
- Preserve domain-specific context: variable names, function signatures, error messages, architectural decisions that the replacement agent will need.
- Do not include raw file contents — the replacement agent can re-read files. Summarize what was learned from reading them.
- Do not repeat the original task — it will be provided separately.

Output: free-form narrative text. No XML tags, no JSON. Write it as a briefing.

No explicit token budget in the prompt text — `MaxOutputTokens: 8192` constrains output at the API level.

**Testing:** The interface has exactly one method. Test doubles are trivial:
```go
type fakeSummarizer struct{ result string; err error }
func (f *fakeSummarizer) Summarize(_ context.Context, _ string) (string, error) {
    return f.result, f.err
}
```

Integration tests for the production `llmSummarizer` implementation are separate and require a real or mocked model — but these are isolated to testing the summarizer alone, not the entire pipeline.

### Component 5: ContextBuilder (Pure Function)

```go
type DeterministicContext struct {
    TaskProgress string // latest TodoWrite checklist, rendered
    RepoState    string // git status/diff
    OriginalTask string // immutable task prompt
}

func BuildContinuationContext(summary, serializedRecent string, det DeterministicContext) string
```

**Responsibility:** Assemble the final continuation prompt from the compacted summary, serialized recent events, and deterministic sections. Pure string concatenation — no LLM, no buffer, no state.

**Output structure:**

```xml
<continuation_context>
<compacted_history>
(LLM-generated narrative summary of older events)
</compacted_history>

<recent_events>
(Full-fidelity serialization of recent events)
</recent_events>

<task_progress>
(Latest TodoWrite checklist — deterministic)
</task_progress>

<repo_state>
(git status --porcelain + git diff --stat — deterministic)
</repo_state>

<task>
(Original task prompt — immutable)
</task>
</continuation_context>

Continue the task above. The <compacted_history> section summarizes earlier work.
The <recent_events> section shows the most recent activity in full detail.
Pick up where the previous context left off. Do not repeat work already completed
or re-attempt approaches documented as failed.
```

**Edge cases:**
- Empty summary (no older events, all turns are recent): omit `<compacted_history>` section entirely
- Empty serializedRecent: omit `<recent_events>` section (shouldn't happen in practice — at least 1 turn)
- Empty TaskProgress: omit `<task_progress>` section
- Empty RepoState: omit `<repo_state>` section

**Inputs:** three strings + one struct
**Outputs:** string
**Error conditions:** none — deterministic concatenation.

**Testing:** Call with literal strings, assert exact output:
```go
got := BuildContinuationContext(
    "The agent read config.go and added retry logic...",
    "=== Turn 18 (5.2s) ===\n[tool] Bash...",
    DeterministicContext{
        TaskProgress: "- [x] Add retry\n- [ ] Add tests",
        RepoState:    "M src/http/client.go",
        OriginalTask: "Add retry logic to the HTTP client",
    },
)
// assert contains <compacted_history>, <recent_events>, etc.
```

No buffer. No LLM. No TurnRecord. Just strings in, string out.

### Composition: Integration in runWithRecovery

The composition site is `runWithRecovery` in `agentrun.go`. The current line 644 (`task = buf.BuildContinuationPrompt()`) is replaced with:

```go
// S6: Composable compaction pipeline
older, recent := SplitWindow(buf.Turns(), recentWindow)
if len(older) > 0 && summarizer != nil {
    serializedOlder := SerializeEvents(older)
    summary, compactErr := summarizer.Summarize(ctx, serializedOlder)
    if compactErr != nil {
        slog.Warn("S6 compaction failed, falling back to S1",
            "station", pm.station, "err", compactErr)
        task = buf.BuildContinuationPrompt()
    } else {
        serializedRecent := SerializeEvents(recent)
        task = BuildContinuationContext(summary, serializedRecent, DeterministicContext{
            TaskProgress: renderTaskProgressFromTurns(buf.Turns()),
            RepoState:    buf.RepoState(),
            OriginalTask: buf.OriginalTask(),
        })
    }
} else {
    // ≤5 turns or no summarizer configured: all turns are recent, serialize raw
    if summarizer == nil {
        task = buf.BuildContinuationPrompt() // S1 fallback
    } else {
        serializedRecent := SerializeEvents(recent)
        task = BuildContinuationContext("", serializedRecent, DeterministicContext{
            TaskProgress: renderTaskProgressFromTurns(buf.Turns()),
            RepoState:    buf.RepoState(),
            OriginalTask: buf.OriginalTask(),
        })
    }
}
```

**How the summarizer reaches `runWithRecovery`:**

`processManager` gains an `summarizer EventSummarizer` field. The coordinator constructs a `llmSummarizer` (wrapping `smallModel` and `runOneShot`) and passes it when creating the process manager. If `smallModel` is nil, `summarizer` is nil → S1 fallback path.

This is the **only** place where the components interact. Each component is imported and called — no registration, no lifecycle, no framework.

### Helper: DeterministicContext Extraction

Two small helpers extract deterministic sections from the buffer for the ContextBuilder:

- `renderTaskProgressFromTurns(turns []TurnRecord) string` — walks turns backward for the latest `LastTodoSnapshot`, renders checklist. This is S1's existing `writeTaskProgress` logic extracted as a pure function over `[]TurnRecord`.
- `buf.RepoState() string` and `buf.OriginalTask() string` — simple getters on ContextBuffer (already exist or trivially added).

### Flash Call Configuration

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxOutputTokens` | 8192 | Thorough narrative for 30+ turns. LLM naturally produces less for shorter sessions. |
| `ThinkingConfig` | Default (not disabled) | Flash can use thinking to analyze the timeline. No artificial constraints. |
| `Temperature` | 0.1 | Faithful summarization, not creative. |

---

## Dependency and Blast-Radius Map

### Direct Changes

| Component | File | Change |
|-----------|------|--------|
| EventStore | `context_buffer.go` | Remove `maxThinkingLen` truncation in `RecordThinking`. Increase `maxInputSummaryLen` to 4096, `maxOutputSummaryLen` to 8192. Add `Turns()`, `RepoState()`, `OriginalTask()` getters. |
| WindowSplitter | `compaction.go` (new) | `SplitWindow()` function |
| EventSerializer | `compaction.go` (new) | `SerializeEvents()` function |
| EventSummarizer | `compaction.go` (new) | `EventSummarizer` interface, `llmSummarizer` struct + constructor, compaction prompt template |
| ContextBuilder | `compaction.go` (new) | `DeterministicContext` struct, `BuildContinuationContext()` function, `renderTaskProgressFromTurns()` helper |
| Integration | `agentrun.go` | `processManager.summarizer` field; composition logic in `runWithRecovery` replacing line 644 |

### Indirect Impact

| Component | Impact |
|-----------|--------|
| `coordinator.go` | Constructs `llmSummarizer` from `smallModel`, passes to `processManager` |
| `context_buffer_test.go` | Update truncation assertions for new limits; add `Turns()` getter tests |
| `compaction_test.go` (new) | Tests for SplitWindow, SerializeEvents, BuildContinuationContext — all with synthetic data |
| `agentrun_recovery_test.go` | Recovery tests inject `fakeSummarizer`; test S6 path and S1 fallback |

### Risk Zones

- **Memory** — Full-fidelity buffer at 50 turns: ~50MB worst case. Invocation-scoped, acceptable.
- **Flash latency** — 200K+ token input at full fidelity → ~5-8s. Worst case ~10s for very long sessions.
- **Compaction quality** — Flash narratives need iterative prompt tuning. Initial version may be imperfect.
- **S1 fallback path** — `BuildContinuationPrompt()` preserved on ContextBuffer. Works with larger captured data — temporal band structure bounds output. S1 tests need truncation assertion updates.
- **New file `compaction.go`** — all pipeline functions in one file. Keep it focused. If it grows past ~400 lines, split by component (unlikely — each function is 20-60 lines).

---

## Implementation Instructions (Handoff Contract)

### What to Build

A composable compaction pipeline for station replacement, decomposed into four independently testable components: WindowSplitter, EventSerializer, EventSummarizer, and ContextBuilder. Plus recording-fidelity changes to the existing ContextBuffer (EventStore).

### In Scope

1. **EventStore changes** — remove thinking truncation, increase tool I/O caps, add `Turns()`/`RepoState()`/`OriginalTask()` getters on `ContextBuffer`
2. **`SplitWindow(turns, recentWindow)` function** — pure partitioning of turn slices
3. **`SerializeEvents(turns)` function** — renders turns to chronological text format
4. **`EventSummarizer` interface + `llmSummarizer` production implementation** — wraps `runOneShot()` with compaction prompt
5. **`BuildContinuationContext(summary, serializedRecent, det)` function** — assembles the composite continuation prompt
6. **`DeterministicContext` struct** — carries task progress, repo state, original task
7. **`renderTaskProgressFromTurns()` helper** — extracted from existing `writeTaskProgress` as pure function
8. **Integration in `runWithRecovery`** — compose pipeline, fallback to S1 on error or nil summarizer
9. **`processManager.summarizer` field** — injected by coordinator
10. **Tests** — table-driven for SplitWindow, SerializeEvents, BuildContinuationContext (all with synthetic data); interface mock for EventSummarizer; integration test for recovery loop with fake summarizer

### Out of Scope

- Changing exhaustion detection logic (`ShouldReplace`, threshold, patterns)
- Changing `runOneShot()` itself
- Changing the recovery loop structure (generation cap, process kill, result reset)
- Deleting S1's `BuildContinuationPrompt()` — preserved as fallback
- Incremental or background compaction
- Compaction prompt tuning beyond initial version
- A/B testing infrastructure between S1 and S6

### Affected Files

- `internal/agent/context_buffer.go` — recording limit changes, new getters
- `internal/agent/compaction.go` (new) — SplitWindow, SerializeEvents, EventSummarizer, llmSummarizer, BuildContinuationContext, DeterministicContext, renderTaskProgressFromTurns, compaction prompt
- `internal/agent/agentrun.go` — `processManager.summarizer`, composition in `runWithRecovery`
- `internal/agent/coordinator.go` — `llmSummarizer` construction
- `internal/agent/compaction_test.go` (new) — pipeline component tests
- `internal/agent/context_buffer_test.go` — updated truncation assertions

### Acceptance Criteria

1. Thinking blocks recorded without any character limit — a 50K-char thinking block round-trips through `RecordThinking` → `Turns()` in full
2. `SplitWindow` with 20 turns and window 5 returns (15 older, 5 recent) — testable with zero infrastructure
3. `SerializeEvents` produces the specified chronological format with turn headers, thinking, tools, errors, results — testable with synthetic TurnRecords
4. `BuildContinuationContext` with string inputs produces XML-structured output with `<compacted_history>`, `<recent_events>`, and deterministic sections — testable with literal strings
5. `BuildContinuationContext` omits `<compacted_history>` when summary is empty (short sessions)
6. When a station exhausts its context and summarizer is available, the continuation prompt uses the S6 pipeline (verifiable by DEBUG log)
7. When `EventSummarizer.Summarize()` returns an error, the system falls back to S1's `BuildContinuationPrompt()` — testable with `fakeSummarizer{err: someErr}`
8. When `processManager.summarizer` is nil, the system uses S1's `BuildContinuationPrompt()` directly
9. The `<task_progress>`, `<repo_state>`, and `<task>` sections are deterministic — present verbatim, never passed through the LLM
10. Sessions with ≤5 turns skip compaction — no `Summarize()` call
11. S1's `BuildContinuationPrompt()` continues to work as fallback — no breakage from recording limit changes
12. All pipeline components testable with zero LLM calls and zero external dependencies

---

## Verification Criteria

1. **Isolated testing**: Each of the four pipeline functions (SplitWindow, SerializeEvents, BuildContinuationContext, renderTaskProgressFromTurns) has at least 3 table-driven test cases using only synthetic data. No test requires a real model, a populated ContextBuffer, or any external dependency.

2. **Full-fidelity recording**: Record a 50K-char thinking block, a 4K tool input, and an 8K tool output via ContextBuffer. Retrieve via `Turns()`. Assert all three round-trip without truncation.

3. **Window split correctness**: Table-driven: 0 turns, 3 turns (< window), 5 turns (= window), 6 turns (> window), 50 turns. Assert older/recent partition sizes and turn sequence numbers.

4. **Serialization format**: Serialize a turn with thinking + 2 tool calls + error + result. Assert output matches the format specification: turn header with elapsed, `[thinking]` block (full), `[tool]` lines with I/O, `[error]` line, `[result]` line, in order.

5. **Context assembly**: Call BuildContinuationContext with known strings. Assert output contains `<compacted_history>`, `<recent_events>`, `<task_progress>`, `<repo_state>`, `<task>` sections with the exact input content. Assert empty summary omits `<compacted_history>`.

6. **Summarizer interface mock**: Create `fakeSummarizer` returning a canned string. Wire into the composition logic. Assert the canned string appears in `<compacted_history>`.

7. **Fallback on summarizer error**: Create `fakeSummarizer` returning an error. Assert the composition falls back to S1's `BuildContinuationPrompt()` output.

8. **Fallback on nil summarizer**: Assert nil summarizer triggers S1 fallback.

9. **Latency**: Measure time from exhaustion detection to replacement station's first output. Must stay under 15s for ≤30 turns.

10. **Cost tracking**: Verify the Flash compaction call's token usage (from `oneShotResult.Usage`) is captured in session cost.

---

## Open Questions Answered

### 1. What's the right summarization prompt?

A narrative-focused prompt instructing Flash to produce a handoff briefing: chronological narrative of what happened, why decisions were made, what failed and why, where work was left off. Free-form text output. No token budget in the prompt — constrained by `MaxOutputTokens: 8192` at the API level. See Component 4 (EventSummarizer) specification.

### 2. Per-type pruning before LLM call?

**No.** S6 records events at full fidelity and lets the LLM compactor decide what's relevant. No pre-filter maintenance surface. Flash's 1M context handles the full serialized events.

### 3. Latency budget

Flash compaction adds ~3-8s. Total replacement (kill + compact + spawn) stays within 15s for typical sessions. Acceptable — station is already dead.

### 4. Failed approaches preservation

The compaction prompt explicitly instructs Flash to call out failed approaches and why they failed. This is part of the narrative, not a separate structured section. S1's deterministic error extraction preserved only in the fallback path.

### 5. Cost tradeoff

~$0.002-0.01 per replacement (Flash). At 3 max: ~$0.006-0.03. Negligible vs. station cost ($0.50-2.00). Quality gain justifies cost.

---

## Assumptions

**A1: Flash model available via coordinator's `smallModel`.**
If nil, summarizer is nil → S1 fallback. No degradation beyond S1 quality.
*Invalidated if:* deployment where only Pro/Ultra is configured.

**A2: Flash produces useful narrative from raw event timelines.**
*Invalidated if:* Flash consistently hallucinates. Requires empirical validation.

**A3: 5 turns is the right recent window.**
Configurable via `recentWindow` parameter. Default 5.
*Invalidated if:* replacement stations need more immediate context.

**A4: Full-fidelity buffer memory acceptable.**
~50MB worst case, invocation-scoped.
*Invalidated if:* memory-constrained environments (unlikely for dev tool).

**A5: Composite (compacted + raw) is better than uniform summary.**
*Invalidated if:* replacement stations struggle to reconcile narrative vs. raw format. Would need explicit transition framing.

**A6: Single file (`compaction.go`) is sufficient for all pipeline components.**
Four functions + one interface + one struct type ≈ 200-300 lines.
*Invalidated if:* file grows past 400 lines. Split by component at that point.

---

## Amendment Log

### Amendment 1 (2026-03-14): Pseudo Context Window

Original design (2026-03-13) used pre-filter + structured XML extraction. Revised to sliding window + LLM compaction following ADK model. Full-fidelity recording, composite timeline output, narrative compaction.

### Amendment 2 (2026-03-14): Composable Pipeline Decomposition

Previous revision (Amendment 1) mixed window splitting, summarization, and context construction into `ContextBuffer` methods and `processManager` closures. Components couldn't be tested without populated buffers or injected function closures.

**What changed:**
1. **ContextBuffer stripped to pure EventStore** — records events, provides getters. No serialization, splitting, or assembly.
2. **Four pipeline components as package-level functions/interface** — `SplitWindow`, `SerializeEvents`, `EventSummarizer`, `BuildContinuationContext`. Each independently testable with synthetic data or literal strings.
3. **`CompactFunc` closure eliminated** — replaced by `EventSummarizer` interface. Cleaner mock pattern (`fakeSummarizer` struct) vs. anonymous function injection.
4. **`BuildCompactedPrompt(ctx, compactFn)` on ContextBuffer eliminated** — the buffer no longer orchestrates compaction. Composition happens at the call site in `runWithRecovery`.
5. **New file `compaction.go`** — all pipeline components in one file. Separate from `context_buffer.go` (recording) and `agentrun.go` (recovery loop).

**Why:** The user requires each component to be testable with zero dependencies on the actual LLM. Pure functions over `[]TurnRecord` and strings achieve this. The previous design's `CompactFunc` closure and `BuildCompactedPrompt` method on ContextBuffer coupled serialization, splitting, and LLM calling into one untestable unit.

**ADRs superseded:** Previous Component Specification (monolithic `ContextBuffer` with `OlderEvents`, `RecentEvents`, `SerializeEvents`, `BuildCompactedPrompt` methods). New ADR-6 (ContextBuffer as pure EventStore) added.

---

## Metadata

2026-03-14 | design | S6 pseudo context window — composable pipeline (#80)
Parent: #42 (context exhaustion epic)
Depends on: S1 (#61, implemented)
Supersedes: original S6 design (2026-03-13), amendment 1 (2026-03-14)
