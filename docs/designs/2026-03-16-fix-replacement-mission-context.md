# Fix: Station Replacement Loses Mission Context (#93)

## Context

### Problem

When a station exhausts its context window and `runWithRecovery` triggers a replacement, the new process receives a continuation prompt that buries the original task and provides no instruction to re-read referenced files. The replacement station inherits a "grocery list" of previous actions but loses orientation — it doesn't know WHY it's doing the work, WHERE the authoritative spec lives, or that it should re-read plan files that the original station had in its context window.

### Root Causes (Three)

**RC1: Original task buried at bottom of continuation prompt.** `BuildContinuationPrompt()` places `<task>` after all temporal band sections (`<previous_work_summary>`, `<previous_work_detail>`, `<recent_activity>`, `<task_progress>`, `<failed_approaches>`, `<repo_state>`). The replacement agent's attention is captured by the verbose work summaries before it reaches the actual task directive. In long sessions (20+ turns), the work summary can be thousands of tokens, pushing the `<task>` section far from the prompt's opening.

Evidence: `context_buffer.go:280-303` — `BuildContinuationPrompt()` writes temporal bands first, then `<task>` near the end, followed by a generic "Continue the task above" instruction.

**RC2: No instruction to re-read referenced files.** The original station typically reads plan files, design docs, or specs early in its session. These file contents exist in the original station's context window but are NOT captured in the continuation buffer (tool output is truncated to 200 chars in S1). The continuation prompt tells the replacement to "pick up where the previous context left off" but never tells it to re-read the plan file that governs the entire implementation.

Evidence: `context_buffer.go:300-301` — the continuation footer says "Continue the task above. Pick up where the previous context left off. Do not repeat work that was already completed." No mention of re-reading referenced files.

**RC3: Skill wrapping and initialPrompt/turnTask interaction on gen > 0.** On replacement (gen > 0), the recovery loop passes the full continuation prompt through `taskBuilder.Build(task, true)` as the `initialPrompt` for `engine.Start()`, but sets `turnTask = task` (raw, no skill wrapping). For streaming (Claude) processes where `firstTurn=true`, `RunTurn` sends `turnTask` via `Send()` while the process is already processing `initialPrompt`. This creates a double-delivery where the continuation prompt reaches the CLI twice: once skill-wrapped via Start, once raw via Send.

Evidence: `harness_recovery.go:62-78`:
```go
initialPrompt := taskBuilder.Build(task, true)  // skill-wrapped continuation
proc, firstTurn, err := ops.GetOrStart(ctx, sessionID, initialPrompt, resumeID)
// ...
turnTask := task                    // raw continuation (gen > 0 skips taskBuilder.Build)
if buf.Generation() == 0 {
    turnTask = taskBuilder.Build(task, firstTurn)
}
```

### Relationship to S6 Design

The existing S6 design (`docs/designs/2026-03-13-S6-llm-summarized-continuation.md`) addresses continuation QUALITY — replacing mechanical temporal-band summaries with LLM-compacted narratives. It does NOT address the mission context problem. S6's `BuildContinuationContext()` places `<task>` at the bottom of its output, reproducing RC1. S6's `DeterministicContext.OriginalTask` is the right data carrier, but its prompt structure has the same ordering problem.

This design fixes the structural/orientation issues. S6 fixes the content quality issues. They are complementary. The fix here should be applied to both S1's `BuildContinuationPrompt()` and will inform S6's `BuildContinuationContext()` when that is implemented.

### Context Manifest

Files examined:
- `internal/agent/harness_recovery.go` (all, 256 lines) — `RecoveryController.Run()`, replacement loop, prompt flow
- `internal/agent/harness_task.go` (all, 30 lines) — `TaskBuilder.Build()`, skill wrapping logic
- `internal/agent/context_buffer.go` (all, 536 lines) — `ContextBuffer`, `BuildContinuationPrompt()`, temporal bands
- `internal/agent/agentrun.go` (lines 29-63, 318-384) — `processManager`, `stationInput`, `getOrStart()`
- `internal/agent/agentrun_recovery_test.go` (all, 504 lines) — recovery test patterns, mock structures
- `docs/designs/2026-03-13-S6-llm-summarized-continuation.md` (all) — S6 pseudo context window design

---

## Solution Pool

### Candidate A: Prompt Restructuring Only (Minimal)

Change `BuildContinuationPrompt()` to reorder sections and add orientation framing. No new types, no structural changes to the recovery loop.

**Changes:**
- Move `<task>` to the TOP of `<continuation_context>`, before any work summaries
- Add a `<replacement_context>` preamble explaining that this is a replacement station
- Add explicit instruction to re-read any files referenced in the original task
- Rewrite the footer instruction to emphasize the original task as the primary directive

**Strengths:**
- Smallest possible diff — only `BuildContinuationPrompt()` changes
- No interface changes, no new types, no recovery loop changes
- Fully backward-compatible: continuation prompt still works the same way, just reordered
- Addresses RC1 and RC2 directly

**Weaknesses:**
- Does not address RC3 (double-delivery / skill wrapping on gen > 0)
- `BuildContinuationPrompt()` accumulates more framing logic without architectural separation
- "Re-read referenced files" instruction is generic — doesn't specify WHICH files

**Fit:**
Solves the most impactful problem (agent orientation) with zero structural risk. Leaves the skill-wrapping issue for a separate fix.

### Candidate B: MissionContext Struct + Recovery Loop Fix

Introduce a `MissionContext` struct that captures the immutable aspects of the station dispatch (original task, skill, station name). Thread it through the recovery loop. Fix the `initialPrompt`/`turnTask` interaction for gen > 0.

**Changes:**
- New `MissionContext` struct with `OriginalTask`, `Skill`, `Station` fields
- `RecoveryController.Run()` captures `MissionContext` once at entry
- `BuildContinuationPrompt()` accepts `MissionContext` — uses it to structure the prompt with mission at the top, progress context below
- For gen > 0: `initialPrompt` is empty/minimal (just starts the CLI), `turnTask` is skill-wrapped continuation via new `TaskBuilder.BuildContinuation()` method
- `TaskBuilder` gains `BuildContinuation(mission MissionContext, continuationBody string) string`

**Strengths:**
- Clean separation of immutable mission context vs. mutable progress context
- Fixes all three root causes (RC1, RC2, RC3)
- `MissionContext` is a natural type that S6's `DeterministicContext` can absorb
- Eliminates double-delivery on gen > 0
- `TaskBuilder.BuildContinuation()` is testable in isolation

**Weaknesses:**
- Larger diff: new type, new method on TaskBuilder, changes to RecoveryController and BuildContinuationPrompt
- `MissionContext` passed through more layers — slight API surface increase
- The "empty initialPrompt for gen > 0" approach requires understanding agentrun's Start/Send interaction, which is backend-specific

**Fit:**
Best architectural solution but larger blast radius. The `MissionContext` concept maps cleanly to the existing code structure.

### Candidate C: TaskBuilder.BuildContinuation() Only

Add a `BuildContinuation()` method to `TaskBuilder` that wraps the buffer's continuation prompt with mission context and skill activation. No new types. Keep `BuildContinuationPrompt()` as-is for the progress body.

**Changes:**
- `TaskBuilder.BuildContinuation(originalTask, continuationBody string) string` — wraps continuation body with mission framing, original task prominence, re-read instruction, and skill activation
- `RecoveryController.Run()` stores `originalTask` (already available as the initial `task` param) and calls `taskBuilder.BuildContinuation(originalTask, buf.BuildContinuationPrompt())` on gen > 0
- For gen > 0: `initialPrompt` is empty, `turnTask` is the wrapped continuation

**Strengths:**
- Uses existing `TaskBuilder` type — no new types
- Addresses all three root causes
- Clear composition: `BuildContinuationPrompt()` provides progress body, `BuildContinuation()` wraps it with mission context
- `BuildContinuation()` testable with string inputs

**Weaknesses:**
- `TaskBuilder` gains two different Build methods with different responsibilities — `Build()` for first turn, `BuildContinuation()` for replacements. Naming must be clear.
- Original task passed as a string parameter rather than as a typed struct — less self-documenting than `MissionContext`
- Doesn't address the "empty initialPrompt" issue unless explicitly added

**Fit:**
Pragmatic middle ground. Leverages existing type, avoids new abstractions, but the two Build methods have subtly different contracts.

### Self-Critique Matrix

| Candidate | Strongest Counter-Argument | Worst-Case | Hidden Cost |
|-----------|---------------------------|------------|-------------|
| **A: Prompt Restructuring** | Does not fix RC3 (double-delivery). Skill wrapping bug remains latent for streaming replacements. | A replacement station that processes the initial prompt (skill-wrapped continuation) AND the turnTask (raw continuation) in sequence, producing a confused two-turn response. | Reordering the prompt may help attention but doesn't guarantee the agent reads referenced files — the instruction is generic. |
| **B: MissionContext Struct** | Introduces a new type and threads it through multiple layers for what is fundamentally a prompt ordering and framing problem. | The "empty initialPrompt for gen > 0" interacts with backend-specific Start() behavior — if a backend requires a non-empty initial prompt, the replacement breaks. | Every future change to mission context (adding fields like `planFilePath`, `issueURL`, etc.) requires updating the struct and all callers. |
| **C: TaskBuilder.BuildContinuation()** | Two Build methods with different contracts on the same type. A developer may call the wrong one. | `BuildContinuation` wraps `BuildContinuationPrompt` output — if the inner prompt format changes (e.g., S6), the outer wrapper must be kept in sync. | `originalTask` as a raw string parameter means no type safety — easy to pass the wrong string (e.g., the continuation prompt instead of the original task). |

---

## Decisions

### ADR-1: Prompt Restructuring with TaskBuilder.BuildContinuation() (Hybrid A+C)

In the context of **replacement stations losing orientation after context exhaustion**, facing **three root causes: buried task, no re-read instruction, and double-delivery on gen > 0**, we decided **to restructure `BuildContinuationPrompt()` to lead with the original task (fixing the prompt body) AND add `TaskBuilder.BuildContinuation()` for skill wrapping and framing on replacements (fixing the recovery loop interaction)**, and neglected **MissionContext struct (adds a type for what two string parameters solve) and prompt-only fix (doesn't address RC3)**, to achieve **a replacement station that sees its mission first, is told to re-read plan files, and receives exactly one well-formed prompt with skill activation**, accepting **that TaskBuilder gains a second Build method with a different contract — mitigated by clear naming and distinct signatures**.

Confidence: **high** — the prompt restructuring is a low-risk text change backed by established prompting practice (lead with the directive). The `BuildContinuation()` method is a pure function testable with string inputs.

Grounding:
- `BuildContinuationPrompt()` (context_buffer.go:280-303) currently writes `<task>` last.
- `TaskBuilder.Build()` (harness_task.go:15-30) already handles skill wrapping — `BuildContinuation()` follows the same pattern.
- `RecoveryController.Run()` (harness_recovery.go:52) already stores `task` as the `buf` parameter — the original task is available at the replacement site.

Rejected: **Candidate B (MissionContext struct)** — introducing a new type is justified when the data crosses multiple boundaries or when type safety prevents misuse. Here, the mission context is two strings (`originalTask`, `skill`) that never leave the recovery loop. A struct adds ceremony without proportional benefit. If mission context grows in the future (adding `planFilePath`, `issueURL`), the struct can be introduced then. **Candidate A alone** — doesn't fix RC3, and the double-delivery bug on gen > 0 is a correctness issue, not just an attention issue.

### ADR-2: Restructure Continuation Prompt — Mission First, Progress Second

In the context of **the replacement agent's attention being captured by verbose work summaries before reaching the actual task**, facing **`<task>` being the last section in `<continuation_context>`**, we decided **to restructure `BuildContinuationPrompt()` so that `<mission>` (containing the original task) is the FIRST element, followed by progress context sections, with a footer that explicitly references the mission as the primary directive**, and neglected **keeping the current order with just a stronger footer instruction (attention still lost in long summaries)**, to achieve **the replacement agent seeing its primary goal before any work history**, accepting **that the prompt format changes — any downstream consumers parsing the XML structure must be updated (none exist outside `RunTurn`)**.

Confidence: **high** — prompt ordering affects LLM attention. Leading with the directive is a well-established pattern. The continuation prompt is consumed only by the replacement station.

### ADR-3: Fix initialPrompt/turnTask Interaction for Gen > 0

In the context of **the recovery loop sending the continuation prompt twice on gen > 0 (once via `engine.Start()`, once via `RunTurn.Send()`)**, facing **the `initialPrompt` variable always receiving the skill-wrapped task regardless of generation**, we decided **to pass an empty `initialPrompt` to `engine.Start()` on gen > 0, and have `turnTask` carry the full skill-wrapped continuation prompt via `TaskBuilder.BuildContinuation()`**, and neglected **keeping the current double-delivery (works accidentally because RunTurn drains the initial prompt's result)**, to achieve **exactly one prompt delivered to the replacement station, containing skill activation + mission context + continuation body**, accepting **that an empty initial prompt for `engine.Start()` means the CLI starts in a "waiting" state — must verify this works for all supported backends (Claude, Codex, OpenCode)**.

Confidence: **medium** — empty initial prompt works for Claude CLI (starts REPL-like, waits for input). Codex and OpenCode are spawn-per-turn (`SequentialSender`), so this path is not taken for them. However, if a future streaming backend requires a non-empty initial prompt, this assumption breaks.

---

## Component Specification

### Component 1: Restructured `BuildContinuationPrompt()` (context_buffer.go)

**What changes:**
The prompt structure changes from:
```
<continuation_context>
  <previous_work_summary>...</previous_work_summary>
  <previous_work_detail>...</previous_work_detail>
  <recent_activity>...</recent_activity>
  <task_progress>...</task_progress>
  <failed_approaches>...</failed_approaches>
  <repo_state>...</repo_state>
  <task>ORIGINAL TASK</task>
</continuation_context>
Continue the task above. Pick up where the previous context left off...
```

To:
```
<continuation_context>
<mission>
ORIGINAL TASK
</mission>

<replacement_notice>
You are a REPLACEMENT station. A previous instance worked on this task but
exhausted its context window. The sections below describe what it accomplished.
IMPORTANT: Re-read any plan files, design documents, or specs referenced in
the mission above before proceeding — the previous instance had these in its
context but they are not included in this handoff.
</replacement_notice>

<previous_work_summary>...</previous_work_summary>
<previous_work_detail>...</previous_work_detail>
<recent_activity>...</recent_activity>
<task_progress>...</task_progress>
<failed_approaches>...</failed_approaches>
<repo_state>...</repo_state>
</continuation_context>

Your primary goal is the <mission> above. Use the work history sections to
understand what was already done. Do not repeat completed work or re-attempt
failed approaches. Start by re-reading any files referenced in the mission.
```

**Key design choices:**
- `<mission>` is the FIRST child of `<continuation_context>` — the replacement agent reads it before anything else
- `<replacement_notice>` immediately after mission — orients the agent to its situation and explicitly calls out the re-read requirement
- `<task>` tag renamed to `<mission>` — signals "this is your primary directive" vs. the old generic `<task>` which reads as just another data section
- Footer instruction references `<mission>` by name and prioritizes re-reading referenced files

**What stays the same:** Temporal band logic (`temporalBands`, `writeBandSummary`, `writeBandDetail`, `writeBandRecent`), task progress extraction, failed approaches, repo state capture. All internal rendering functions are unchanged.

### Component 2: `TaskBuilder.BuildContinuation()` (harness_task.go)

A new method on `TaskBuilder` that composes the final prompt for replacement stations:

**Responsibility:** Wrap a continuation prompt body with skill activation and station-specific suffixes. Analogous to `Build()` but for gen > 0.

**Contract:**
- Input: `originalTask string` (preserved from initial dispatch), `continuationBody string` (from `BuildContinuationPrompt()`)
- Output: string — the complete prompt for the replacement station

**Behavior:**
- If `tb.skill != ""`: prepend skill loading instruction (backend-aware, same patterns as `Build()`)
- Append continuation body after skill instruction
- Station-specific suffixes (e.g., "Always provide the full path to the file you create" for draft/design)

**Why a separate method:** `Build()` handles first-turn task transformation (skill activation + raw task). `BuildContinuation()` handles replacement-turn composition (skill activation + continuation body). The inputs and framing differ. A single method with a `generation int` parameter would conflate these contracts.

### Component 3: Recovery Loop Changes (harness_recovery.go)

**What changes in `RecoveryController.Run()`:**

1. **Preserve original task separately.** Store `originalTask := task` at entry. The `task` variable mutates through the loop (overwritten with continuation prompt on gen > 0). `originalTask` is immutable.

2. **Fix initialPrompt for gen > 0.** On gen > 0, `initialPrompt` is empty string. The CLI starts in a waiting state. The full prompt (with skill wrapping) is delivered via `turnTask` / `RunTurn.Send()`.

3. **Build turnTask for gen > 0 via `BuildContinuation()`.** Instead of `turnTask = task` (raw continuation), use `turnTask = taskBuilder.BuildContinuation(originalTask, continuationBody)`.

4. **Pass `originalTask` to `NewContextBuffer`.** The buffer already receives the task, but making it explicit that this is the ORIGINAL task (not a continuation prompt) prevents confusion on gen > 0 iterations where `task` is overwritten.

**Pseudocode of the changed recovery loop:**
```
originalTask := task
buf := NewContextBuffer(originalTask)

for range maxGenerations + 1 {
    var initialPrompt string
    if buf.Generation() == 0 {
        initialPrompt = taskBuilder.Build(task, true)
    } else {
        initialPrompt = ""  // gen > 0: empty start, full prompt via Send
    }

    proc, firstTurn, err := ops.GetOrStart(ctx, sessionID, initialPrompt, resumeID)

    turnTask := task
    if buf.Generation() == 0 {
        turnTask = taskBuilder.Build(task, firstTurn)
    } else if firstTurn {
        turnTask = taskBuilder.BuildContinuation(originalTask, task)
    }

    // ... rest of turn execution unchanged ...

    // On replacement:
    continuationBody := buf.BuildContinuationPrompt()
    task = continuationBody  // task var now holds continuation body
}
```

### Interaction Between Components

```
RecoveryController.Run(task="implement plan at /path/plan.md", ...)
  │
  ├── originalTask = task                          (immutable, preserved)
  ├── buf = NewContextBuffer(originalTask)         (buffer knows original task)
  │
  ├── [Gen 0] ─── Build(task, firstTurn)           (skill-wrapped task)
  │                │
  │                └── Station runs, exhausts context
  │
  ├── buf.BuildContinuationPrompt()                (mission-first structure)
  │     │
  │     ├── <mission>implement plan at /path/plan.md</mission>
  │     ├── <replacement_notice>Re-read plan files...</replacement_notice>
  │     └── <previous_work>...</previous_work>
  │
  └── [Gen 1] ─── BuildContinuation(originalTask, continuationBody)
                   │
                   └── "Load your skill and then: <continuation_context>..."
                       (single prompt, delivered via RunTurn.Send)
```

---

## Dependency and Blast-Radius Map

### Direct Changes

| File | Change |
|------|--------|
| `internal/agent/context_buffer.go` | Restructure `BuildContinuationPrompt()`: `<mission>` first, `<replacement_notice>`, rewritten footer. ~20 lines changed in one method. |
| `internal/agent/harness_task.go` | Add `BuildContinuation(originalTask, continuationBody string) string` method. ~15 lines. |
| `internal/agent/harness_recovery.go` | Store `originalTask`, empty `initialPrompt` for gen > 0, call `BuildContinuation()`. ~10 lines changed in `Run()`. |

### Indirect Impact

| File | Impact |
|------|--------|
| `internal/agent/context_buffer_test.go` | Update `BuildContinuationPrompt()` assertions for new section ordering (mission first, replacement notice). |
| `internal/agent/agentrun_recovery_test.go` | Update gen > 0 assertions: `sends1[0]` should contain `<mission>` not just `<continuation_context>`. Verify empty `initialPrompt` for gen > 0 via `engine.starts[1].Prompt == ""`. |

### Risk Zones

- **Empty initialPrompt for gen > 0** — assumes Claude CLI handles `Start(Session{Prompt: ""})` by entering a wait-for-input state. If any streaming backend requires a non-empty start prompt, this breaks. Codex and OpenCode are spawn-per-turn and don't hit this path.
- **Prompt format change** — any tests asserting exact continuation prompt content will break. These are localized to `context_buffer_test.go` and `agentrun_recovery_test.go`.
- **S6 compatibility** — S6's `BuildContinuationContext()` uses a similar XML structure. When S6 is implemented, it should adopt the mission-first ordering. This design's `<mission>` and `<replacement_notice>` sections should be adopted into S6's `DeterministicContext` concept.

---

## Implementation Instructions (Handoff Contract)

### What to Build

Fix the station replacement continuation prompt so the replacement station sees its primary goal first, is told to re-read plan files, and receives exactly one well-formed prompt with skill activation.

### In Scope

1. **Restructure `BuildContinuationPrompt()`** — `<mission>` section first (containing original task), `<replacement_notice>` second (with re-read instruction), then existing temporal band sections, then rewritten footer referencing `<mission>` as primary directive.

2. **Add `TaskBuilder.BuildContinuation(originalTask, continuationBody string) string`** — wraps continuation body with skill activation (backend-aware) and station-specific suffixes. Same skill-wrapping patterns as `Build()`.

3. **Fix `RecoveryController.Run()` for gen > 0** — store `originalTask` at entry; for gen > 0, pass empty `initialPrompt` to `getOrStart()` and build `turnTask` via `BuildContinuation()`.

4. **Update tests** — assertion changes in `context_buffer_test.go` and `agentrun_recovery_test.go` for new prompt structure and gen > 0 behavior.

### Out of Scope

- Changing exhaustion detection logic (`ShouldReplace`, thresholds)
- Plan file path extraction heuristics (the re-read instruction is generic, not file-specific)
- S6 implementation (this design informs S6 but doesn't implement it)
- Changing `Build()` behavior for gen 0
- Adding a `MissionContext` struct (defer until mission context grows beyond two fields)

### Affected Files

- `internal/agent/context_buffer.go` — `BuildContinuationPrompt()` restructure
- `internal/agent/harness_task.go` — new `BuildContinuation()` method
- `internal/agent/harness_recovery.go` — `originalTask` preservation, gen > 0 prompt flow
- `internal/agent/context_buffer_test.go` — updated assertions
- `internal/agent/agentrun_recovery_test.go` — updated assertions, new test for gen > 0 prompt correctness

### Acceptance Criteria

1. The continuation prompt's FIRST XML section is `<mission>` containing the original task text verbatim.
2. The continuation prompt includes a `<replacement_notice>` section that explicitly instructs the replacement to re-read any files referenced in the mission.
3. On gen > 0, `engine.Start()` receives an empty prompt (verifiable via `engine.starts[N].Prompt`).
4. On gen > 0 with streaming backend, `RunTurn.Send()` delivers exactly one prompt containing both skill activation and continuation body.
5. On gen > 0, the sent prompt includes skill wrapping (e.g., "Load your X skill and then: ...").
6. The footer instruction references `<mission>` as the primary directive and prioritizes re-reading referenced files.
7. Gen 0 behavior is unchanged — `Build()`, `initialPrompt`, and `turnTask` all work as before.
8. S1 fallback (no S6 summarizer) produces the new mission-first prompt structure.

---

## Verification Criteria

1. **Mission prominence**: Build a `ContextBuffer` with 20 turns of recorded activity, call `BuildContinuationPrompt()`. Assert that `<mission>` appears before `<previous_work_summary>` in the output string.

2. **Re-read instruction present**: Assert the continuation prompt contains text instructing the replacement to re-read plan files / design documents / specs referenced in the mission.

3. **Empty initialPrompt on gen > 0**: In `TestRunWithRecovery_StreamingUsesRunTurnOnReplacement`, assert `engine.starts[1].Prompt == ""` (the replacement process starts with an empty prompt).

4. **Single delivery on gen > 0**: Assert `proc1.Sends()` has exactly one entry containing both skill activation prefix and `<continuation_context>`.

5. **Skill wrapping on replacement**: Assert that `proc1.Sends()[0]` contains the skill prefix (e.g., "Load your X skill and then:") when the station has a configured skill.

6. **Original task preserved verbatim**: The `<mission>` section in the continuation prompt contains the exact original task string, not a truncated or modified version.

7. **Gen 0 unchanged**: Existing tests for gen 0 (first turn, existing process, spawn-per-turn) continue to pass without modification to their assertions.

8. **BuildContinuation testable in isolation**: `TaskBuilder.BuildContinuation("original task", "<continuation_context>...")` produces output containing skill prefix, original task text, and continuation body — testable with plain string assertions.

---

## Assumptions

**A1: Claude CLI handles empty initial prompt by entering wait-for-input state.**
For gen > 0, `engine.Start(Session{Prompt: ""})` must start the CLI without processing a prompt. The first real prompt arrives via `Send()`.
*Invalidated if:* Claude CLI requires a non-empty initial prompt and errors on empty string. Mitigation: use a minimal prompt like `"Continuing from previous context."` instead of empty.

**A2: Generic re-read instruction is sufficient (no file path extraction needed).**
The continuation prompt says "re-read any plan files or design documents referenced in the mission" without specifying which files. The replacement agent can parse the mission text and find file paths itself.
*Invalidated if:* Replacement agents consistently fail to identify referenced files from natural language. Would need a file path extraction heuristic.

**A3: `<mission>` tag is unambiguous to the replacement agent.**
Renaming `<task>` to `<mission>` improves directive clarity. The replacement agent treats `<mission>` as its primary goal.
*Invalidated if:* The agent treats `<mission>` as metadata rather than a directive. Would need stronger framing or a different tag name.

**A4: Spawn-per-turn backends (Codex, OpenCode) don't hit the gen > 0 streaming path.**
These backends implement `SequentialSender`, so their recovery path returns after the first turn (`RemoveFromPool`). The empty-initialPrompt fix only affects streaming backends.
*Invalidated if:* A spawn-per-turn backend is added that also supports multi-turn recovery (unlikely given the spawn-per-turn contract).

---

## S6 Compatibility Notes

When S6 is implemented, the following changes from this design should be adopted:

1. **`DeterministicContext` should include `<mission>` and `<replacement_notice>`** — these become part of the deterministic (non-LLM) sections of the composite continuation prompt.
2. **`BuildContinuationContext()` should place `<mission>` first** — before `<compacted_history>` and `<recent_events>`.
3. **`TaskBuilder.BuildContinuation()`** can wrap S6's output the same way it wraps S1's output — the method is agnostic to the continuation body's internal format.
4. **The compaction prompt should reference the original task** — Flash's summary instruction can note that the original task is provided separately, so the summary should focus on work done rather than restating the goal.

---

## Metadata

2026-03-16 | design | Fix replacement mission context loss (#93)
Parent: #42 (context exhaustion epic)
Depends on: S1 (#61, implemented)
Related: S6 design (docs/designs/2026-03-13-S6-llm-summarized-continuation.md)
