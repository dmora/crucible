# Crucible Eval — Test Dataset

5 tasks against crucible-pro. Run each through Crucible. I analyze the ADK database after.

## Measuring

After each run, from the ADK database:
- Total dispatches (station tool calls)
- Station sequence (actual routing)
- Operator messages (how many, how many were corrections)
- Ship outcome (PR created, denied, or not attempted)
- Verify outcome (tests pass/fail)
- Wall clock time

## Task 1: Bug Fix (Size S)

**Issue:** #47 — append station isolation reminder to all station tool descriptions

**Work order:**
> The supervisor dispatches to stations with lazy references like "see the user's instructions" instead of including actual content. Fix this by appending a reminder to every station tool description that stations are isolated processes and cannot see the operator's conversation. The supervisor must include all relevant information in the task field.

**Acceptance criteria:**
- [ ] Every station tool description includes an isolation reminder
- [ ] `go build` passes
- [ ] `go test ./...` passes
- [ ] Change is in `internal/agent/agentrun.go` or `internal/config/config.go`

**Expected route:** draft → build → verify → ship (4 stations, skip design/inspect for a small bug fix)

---

## Task 2: Small Feature (Size S)

**Issue:** #67 — station tool invocation timeout

**Work order:**
> Add a configurable Timeout field to StationConfig. Wrap the station tool execution with context.WithTimeout. Default timeouts: 5 minutes for plan stations, 15 minutes for act stations, 10 minutes for gated stations. If timeout fires, return a timeout error result to the supervisor.

**Acceptance criteria:**
- [ ] `Timeout` field exists on `StationConfig` in `internal/config/config.go`
- [ ] Station tool call in `internal/agent/agentrun.go` uses `context.WithTimeout`
- [ ] Default timeouts set per station mode
- [ ] `go build` passes
- [ ] `go test ./...` passes

**Expected route:** draft → inspect → build → verify → ship (5 stations)

---

## Task 3: Bug Fix with Investigation (Size S)

**Issue:** #8 — supervisor breaks role seal

**Work order:**
> The supervisor sometimes writes code directly instead of delegating to a station. Investigate the system prompt template at internal/agent/templates/coder.md.tpl and strengthen the role seal. The supervisor should NEVER use file editing tools directly — only station tools, ask_user, todos, and search/read tools.

**Acceptance criteria:**
- [ ] System prompt template explicitly forbids direct file editing
- [ ] Tool set excludes write/edit tools for the supervisor (or prompt clearly states they must not be used)
- [ ] No changes to station behavior
- [ ] `go build` passes
- [ ] `go test ./...` passes

**Expected route:** design → draft → build → review → verify → ship (6 stations — needs design to investigate the problem first)

---

## Task 4: Infrastructure (Size S)

**Issue:** #68 — pipeline-scoped circuit breaker

**Work order:**
> Add a pipeline circuit breaker that tracks consecutive station failures within a session. After N consecutive failures (default 3), halt the pipeline and trigger ask_user. Reset counter on any successful station dispatch. Wire it into the station tool completion path in internal/agent/agentrun.go.

**Acceptance criteria:**
- [ ] New circuit breaker type exists (counter + threshold + reset)
- [ ] Wired into station tool result handling
- [ ] Triggers ask_user on threshold
- [ ] Resets on success
- [ ] `go build` passes
- [ ] `go test ./...` passes
- [ ] Has at least one test

**Expected route:** draft → inspect → build → review → verify → ship (5 stations)

---

## Task 5: Refactor (Size S)

**Issue:** #69 — verdict-aware steering

**Work order:**
> Extend the steering plugin to inject structured failure feedback when a station returns VerdictFailed. Currently steering carries plain text routing hints. Add a StationFeedback struct with: Station, Verdict, FailureType (test_failure, review_rejection, context_exhaustion, crash), Issues list, and Suggestion. When a station fails, the steering plugin should construct a StationFeedback and inject it into the next supervisor turn as structured data.

**Acceptance criteria:**
- [ ] `StationFeedback` struct defined
- [ ] `steering_plugin.go` constructs feedback from station verdict
- [ ] Feedback injected into supervisor context on failure
- [ ] Non-failure verdicts still use existing plain text steering
- [ ] `go build` passes
- [ ] `go test ./...` passes

**Expected route:** draft → inspect → build → review → verify → ship (5 stations)
