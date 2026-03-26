# Design: Station Route Enforcement

`2026-03-24 | design | station-route-enforcement`

## Context

### Problem

The supervisor skips quality gates 52% of the time. `build→build` is 143 transitions across 692 dispatches (21% of all routing). The supervisor enters reactive loops without review. Steering hints are advisory — the supervisor ignores them.

### What We Want

Stations declare requirements. The station tool rejects calls when requirements aren't met. The supervisor can't skip steps because the tool won't execute. Same enforcement pattern as the existing gate system (hard reject, `Abort: true`), but for route compliance instead of operator permission.

### Requirements

1. **Users can reconfigure it.** Station requirements live in `StationConfig`, overridable per-station via `crucible.json` field-level merge.
2. **Simple to understand.** Look at one station's config, understand what it needs to run.
3. **Hard enforcement.** Go code in the tool, not a prompt suggestion. Returns DENIED like the gate system.

### What Exists

| Component | File | How it works |
|---|---|---|
| Gate check | `agentrun.go:514` | `pm.gate.Check()` → returns approved/denied before dispatch |
| Dispatch log | `process_state.go` | Records every station call with verdict (Done/Failed/Canceled), persists to ADK state |
| StationConfig | `config.go:386` | Field-level merge from defaults → global → user → project config |
| Steering | `steering_plugin.go` | Advisory text injection into system instruction. Supervisor can ignore. |

### Files Examined

- `internal/agent/agentrun.go` (lines 505-569) — newStationTool, 5-step pipeline
- `internal/agent/process_state.go` (lines 301-393) — DispatchEntry, verdicts, dispatch log
- `internal/agent/gate.go` (lines 14-33) — checkGate pattern
- `internal/agent/harness_gate.go` — GateController struct
- `internal/agent/agent.go` (lines 201-257, 469-475) — station init, tool registration
- `internal/agent/steering_plugin.go` — post-station routing hints
- `internal/config/config.go` (lines 386-418, 1012-1082) — StationConfig, DefaultStations
- `internal/config/load.go` (lines 27-92, 581-609) — config load/merge chain

---

## Solution Pool

### Candidate A: Requires + Produces (Token Model)

Stations declare what they produce on success and what they require to run. Tokens are tracked in session state. Consuming a requirement means it must be re-produced before re-use.

```json
{
  "stations": {
    "draft":   { "produces": "plan" },
    "inspect": { "requires": ["plan"],    "produces": "approved_plan" },
    "build":   { "requires": ["approved_plan"], "produces": "changes" },
    "review":  { "requires": ["changes"], "produces": "reviewed" },
    "verify":  { "requires": ["reviewed"], "produces": "verified" },
    "ship":    { "requires": ["verified"] }
  }
}
```

**How it solves build→build:** Build produces `changes`. Review consumes `changes` and produces `reviewed`. If build tries to run again, `approved_plan` was consumed by the first build — so build is blocked until draft→inspect runs again. Actually this creates a problem: you can't iterate build→review→build because the plan gets consumed.

**Strengths:** Composable — each station is self-contained. The token graph is a clear visual model. Users can define custom tokens for custom flows.

**Weaknesses:** Token lifecycle (consumed? persistent? one-time?) adds conceptual weight. "Consumed on success" creates problems for iterative loops (build→review→build→review). The model fights the natural pattern of re-dispatching.

**Self-critique:** This is a Petri net. Petri nets are powerful but hard to reason about for simple cases. The user asked for simple. Explaining "tokens are consumed when a station succeeds" to a Crucible user who just wants to enforce review-after-build is over-engineering.

### Candidate B: Requires + GatedBy (Station References)

Two simple fields per station. `requires` = what must have run before I can run (first time). `gatedBy` = what must run after me before I can run again.

```json
{
  "stations": {
    "build":  { "requires": ["draft"], "gatedBy": "review" },
    "verify": { "requires": ["build"] },
    "ship":   { "requires": ["verify"], "gate": true }
  }
}
```

**How it solves build→build:** Build has `gatedBy: "review"`. After build completes with VerdictDone, the enforcer checks: has review run since the last successful build? If not, reject with "review must run before build can dispatch again."

**How `requires` works:** Before build runs, check the dispatch log: has `draft` completed with VerdictDone at any point in this session? If not, reject with "draft must complete before build can run."

**How `gatedBy` works:** After build succeeds, a flag is set: "build needs review." On the next build call, the enforcer checks the flag. If review has run since, the flag clears. If not, reject.

**Strengths:** Two fields, both referencing station names the user already knows. Look at one station's config, understand the constraints. Users override one field without understanding the whole system.

**Weaknesses:** `gatedBy` only supports one station (not a list). What if you want build gated by both review AND verify? Could make it a list, but that increases complexity.

**Self-critique:** `gatedBy` is a re-entry constraint, which is harder to explain than `requires`. "What must run after me before I run again" is a double-negative concept. Users might confuse `gatedBy` with the existing `gate` field (operator permission). The naming needs work.

### Candidate C: Transition Rules (Allowed Next)

Each station declares which stations can follow it. The enforcer tracks the last completed station and rejects calls that aren't in the allowed-next list.

```json
{
  "stations": {
    "draft":   { "allowedNext": ["inspect"] },
    "inspect": { "allowedNext": ["draft", "build"] },
    "build":   { "allowedNext": ["review", "verify"] },
    "review":  { "allowedNext": ["build", "verify"] },
    "verify":  { "allowedNext": ["ship", "build"] },
    "ship":    { "allowedNext": [] }
  }
}
```

**How it solves build→build:** Build's `allowedNext` is `["review", "verify"]`. Build is not in its own allowed-next list. After build completes, the next station call must be review or verify — build is rejected.

**Strengths:** Very explicit — the graph is right there in the config. Easy to visualize the pipeline.

**Weaknesses:** Adding a station requires updating every other station's `allowedNext` list. If you add a `lint` station, you need to update build, review, verify to include it. Not composable — changing one station touches many others. Also: what's the "first" allowed station? You need a separate entry point concept.

**Self-critique:** This is an adjacency list. It's simple for small graphs but becomes a maintenance burden as stations grow. The user requirement was "hack one station" — this model requires hacking multiple stations for any change. Also doesn't handle the first dispatch (what's "last completed" when nothing has run?).

---

## Self-Critique Matrix

| | A: Token Model | B: Requires + GatedBy | C: Transition Rules |
|---|---|---|---|
| **Strongest counter** | Consumed tokens break iterative loops (build→review→build) | `gatedBy` naming confuses with existing `gate` field | Adding a station requires editing every other station |
| **Worst case** | Token lifecycle bugs cause stations to be permanently blocked | User sets `gatedBy: "inspect"` on build, blocks legitimate build→review→build flow | Large station count makes `allowedNext` unmanageable |
| **Hidden cost** | Token state must persist, be debuggable, and handle edge cases (session reload, cancellation) | Two concepts (`requires` vs `gatedBy`) where one might suffice | Entry point ambiguity — what can run first? |

---

## Decisions

### ADR-1: Use the Requires model with simple station references

> In the context of **enforcing station routing to prevent the supervisor from skipping quality gates**, facing **three candidate models (tokens, station refs, transition rules)**, we decided **station references with a `requires` field**, and neglected **the token model (too complex for the problem) and transition rules (not composable — adding a station touches all others)**, to achieve **a simple, per-station configuration that users can override without understanding the whole pipeline**, accepting **that re-entry constraints (build→review→build) need a second mechanism beyond basic `requires`**.

Confidence: **high** — the gate system already proves that per-station hard enforcement works in this codebase. `requires` extends the same pattern.

### ADR-2: Express re-entry constraints as `afterDone` rather than `gatedBy`

> In the context of **preventing build→build chains (143 occurrences, 21% of routing)**, facing **naming confusion between `gatedBy` (route enforcement) and `gate` (operator permission)**, we decided **`afterDone` as the field name — a list of stations that must run after this station succeeds before it can dispatch again**, and neglected **`gatedBy` (confuses with `gate`) and `nextRequired` (ambiguous direction)**, to achieve **clear intent: "after build is done, these must run before build runs again"**, accepting **that `afterDone` is a second concept alongside `requires` — two fields instead of one**.

Confidence: **medium** — naming is subjective. `afterDone` reads naturally ("after done, review must run") but users might still need an explanation. The existing `gate` field precedent means we need clear differentiation.

### ADR-3: Enforcement lives in the station tool, same as the gate check

> In the context of **where to put the route enforcement code**, facing **options: in the station tool (alongside gate), as an ADK plugin (like circuit breaker), or in the supervisor prompt**, we decided **in the station tool, before the gate check**, and neglected **ADK plugin (can be ignored, as circuit breaker showed) and prompt injection (steering already proves this fails)**, to achieve **hard enforcement that the supervisor cannot bypass — the tool returns DENIED before dispatch is even created**, accepting **that this adds logic to `newStationTool()` which is already the busiest function in the agent layer**.

Confidence: **high** — the gate pattern at `agentrun.go:514` is proven. Route enforcement is the same shape: check a condition, return DENIED if not met. Putting it in the same place means the same error handling, the same UI behavior, the same abort flow.

### ADR-4: Failed/canceled dispatches don't trigger `afterDone` constraints

> In the context of **allowing build retries after failures without requiring review first**, facing **the question of which verdicts activate route constraints**, we decided **only VerdictDone activates `afterDone` constraints — VerdictFailed and VerdictCanceled are ignored**, and neglected **treating all completions equally (would block legitimate retries)**, to achieve **a model where the supervisor can retry a failed station freely but must route through quality gates after a successful one**, accepting **that a station producing bad output with VerdictDone will still trigger the constraint — the enforcer can't judge output quality, only completion status**.

Confidence: **high** — this matches the existing dispatch log semantics. VerdictDone means the process completed, not that the work is good. Review's job is to judge quality, and the enforcer's job is to ensure review runs.

---

## Component Specification

### Route Enforcer

A new component that sits between the station tool entry point and the gate check. It reads the dispatch log for the current session and the station's requirements from config.

**Responsibilities:**
- Before a station dispatches, check if `requires` stations have completed in this session
- Before a station dispatches, check if `afterDone` constraints are satisfied (required stations have run since the last successful dispatch of this station)
- Return a descriptive rejection message telling the supervisor exactly what to dispatch next
- Do nothing for stations with no requirements (design, draft by default)

**Data flow:**
- Reads: dispatch log (already per-session, already persisted to ADK state)
- Reads: station config (requires, afterDone fields)
- Does not write anything — enforcement is read-only on existing state

**Error messages** are critical for the supervisor. The rejection must tell the LLM exactly what to do:
- `"Station 'build' requires 'draft' to complete first. Dispatch draft before build."`
- `"Station 'build' completed successfully. 'review' must run before build can dispatch again. Dispatch review."`

### Config Changes

Two new fields on `StationConfig`:

- `Requires []string` — station names that must have VerdictDone in the session's dispatch log before this station can run
- `AfterDone []string` — station names that must have VerdictDone MORE RECENTLY than this station's last VerdictDone before this station can run again

Both default to empty (no enforcement) for backward compatibility. Users override per-station via `crucible.json`.

### Default Configuration

```json
{
  "draft":   { "requires": [],           "afterDone": [] },
  "inspect": { "requires": [],           "afterDone": [] },
  "build":   { "requires": ["draft"],    "afterDone": ["review"] },
  "review":  { "requires": ["build"],    "afterDone": [] },
  "verify":  { "requires": ["review"],   "afterDone": [] },
  "ship":    { "requires": ["verify"],   "afterDone": [], "gate": true }
}
```

What this enforces:
- Build requires draft to have completed first (plan before code)
- Build requires review to have run since the last successful build (no build→build chains)
- Review requires build (can't review without code changes)
- Verify requires review (can't verify unreviewed code)
- Ship requires verify (can't ship without tests passing)
- Design and draft have no constraints (entry points)

What users can change:
- Remove `"afterDone": ["review"]` from build if they want autonomous build loops
- Add `"requires": ["inspect"]` to build if they want mandatory plan inspection
- Add `"afterDone": ["verify"]` to build if they want tests after every build
- Clear all requirements for a quick-fix workflow: just build→ship

### Dispatch Log Query

The enforcer queries the dispatch log with two operations:

1. **Has station X completed in this session?** — scan dispatch log for any entry where `Station == X` and `Verdict == VerdictDone`
2. **Has station X completed more recently than station Y?** — compare the highest `Seq` value of X's VerdictDone entries vs Y's VerdictDone entries

Both are scans over the in-memory dispatch log (already loaded per session). No new storage, no new persistence.

---

## Dependency and Blast-Radius Map

**Direct changes:**
- `internal/config/config.go` — add `Requires` and `AfterDone` fields to `StationConfig`, add defaults
- `internal/agent/agentrun.go` — add route enforcement check before gate in `newStationTool()`

**Indirect impact:**
- `internal/agent/agent.go` — may need to pass station config or dispatch log access to `newStationTool()` (currently only gets processManager, sessionID, description, perms, holdFlag, notifier, turnAbort)
- `internal/agent/process_state.go` — dispatch log query functions (may need new helpers, or enforcement reads the log directly)
- Config merge chain — new fields auto-merge from defaults (no changes to load.go needed)

**Risk zones:**
- `newStationTool()` is already the busiest function. Adding another check before the gate increases its cyclomatic complexity. Keep the enforcer as a separate function called from the closure.
- Dispatch log is in-memory with periodic persistence. If the enforcer reads stale state (race between dispatch log write and enforcement read), a station could be incorrectly blocked or allowed. The dispatch log is already mutex-protected per session, so this should be safe.

---

## Implementation Instructions (Handoff Contract)

### What to Build

A route enforcement check that runs before the gate check in `newStationTool()`. When requirements aren't met, the tool returns DENIED with a descriptive message, same as the gate rejection pattern.

### In Scope

- `Requires` and `AfterDone` fields on `StationConfig`
- Default values for the 7 existing stations
- Route enforcement function that reads the dispatch log
- Descriptive rejection messages that tell the supervisor what to dispatch next
- Unit tests for the enforcement logic

### Out of Scope

- Changing the dispatch log structure (use it as-is)
- UI changes (the TUI already shows DENIED results from the gate)
- Steering plugin changes (enforcement replaces the need for advisory steering for routing)
- ADK plugin approach (enforcement is in the tool, not a plugin)
- Recipe/route templates (enforcement is per-station, not per-pipeline)

### Affected Files

- `internal/config/config.go` — StationConfig struct, DefaultStations map
- `internal/agent/agentrun.go` — newStationTool closure, new enforcement function
- `internal/agent/process_state.go` — possibly new query helpers for dispatch log
- New test file for enforcement logic

### Acceptance Criteria

- Build station cannot dispatch without draft having completed (VerdictDone) in the session
- Build station cannot dispatch again after a successful build without review having run
- Failed builds (VerdictFailed) can retry immediately without review
- Ship station cannot dispatch without verify having completed
- Stations with empty `requires` and `afterDone` dispatch freely (backward compatible)
- User can override any station's `requires` and `afterDone` via `crucible.json`
- Rejection messages name the specific station that needs to run next
- `go build` and `go test ./...` pass

---

## Verification Criteria

- Given a session where draft has not run, calling build returns a rejection naming draft
- Given a session where build succeeded, calling build again without review returns a rejection naming review
- Given a session where build failed, calling build again succeeds (retry allowed)
- Given a session where build succeeded and review succeeded, calling build again succeeds
- Given a modified config where build has no `afterDone`, build→build dispatches freely
- Given a modified config where ship requires `["review", "verify"]`, ship rejects unless both have run
- Rejection messages match the existing gate DENIED format so the supervisor handles them identically

---

## Assumptions

**A1:** The dispatch log is always available in memory during station tool execution.
*Invalidated if:* dispatch log is lazily loaded or cleared between turns.

**A2:** The supervisor will respond to DENIED messages by dispatching the named station, same as it does for gate denials.
*Invalidated if:* the supervisor ignores DENIED messages from route enforcement (would need eval data to confirm).

**A3:** Users understand station names well enough that `"requires": ["draft"]` is self-explanatory.
*Invalidated if:* user testing shows confusion about what the config means.

**A4:** Two fields (`requires` + `afterDone`) are sufficient to express all needed routing constraints.
*Invalidated if:* we discover constraints that neither field can express (e.g., "build OR design must have run," OR-logic).
