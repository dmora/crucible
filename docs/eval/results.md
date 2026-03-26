# Crucible Eval Results

## Dataset

- **3 projects:** crucible-pro, adk-go, huli
- **28 sessions, 692 station dispatches**
- **Period:** 2026-03-17 to 2026-03-23
- **Plus:** post-Crucible review sessions via Claude Code (operator command center)

---

## Aggregate Metrics (692 dispatches, 28 sessions)

| Metric | adk-go | crucible-pro | huli | **Combined** |
|---|---|---|---|---|
| Sessions | 4 | 11 | 13 | **28** |
| Dispatches | 84 | 258 | 346 | **692** |
| Prompt references | 0% | 11.6% | 4.9% | **~7%** |
| Dispatch quality | 100% | 88% | 86% | **~88%** |
| Wasted dispatches | 15% | 14.7% | 36% | **~25%** |
| Gate skip rate | 72%* | 12% | 71% | **~52%** |
| PR completion | 75% | 73% | 54% | **~65%** |
| Correction rate | 12% | 16% | 25% | **~18%** |

*adk-go's 72% was one autonomous overnight session (f5fea522)

### Station Usage

| Station | Dispatches | % of total |
|---|---|---|
| build | 279 | 40% |
| draft | 108 | 16% |
| inspect | 91 | 13% |
| verify | 73 | 11% |
| ship | 72 | 10% |
| review | 55 | 8% |
| design | 14 | 2% |

### Per-Station Reliability

| Station | adk-go | crucible-pro | huli |
|---|---|---|---|
| verify | 100% | 100% | 98% |
| build | 100% | 94% | 96% |
| review | 36% clean pass* | 78% | 95% |
| inspect | 100% | 83% | 90% |
| draft | 100% | 86% | 83% |
| ship | 80% | 38%** | 26%** |
| design | — | 67% | 100% |

*adk-go review: 100% technical success but only 36% clean pass (rest found blocking issues requiring fixes)
**Ship failures are almost entirely operator DENIED, not station failures

### Top Transitions (what the supervisor actually does)

| Transition | Count | What it means |
|---|---|---|
| build → build | 143 | Supervisor re-dispatches without quality gate |
| draft → inspect | 67 | Plan iteration (good) |
| build → verify | 62 | Standard exit path (good) |
| inspect → draft | 47 | Inspect rejects, draft revises (good) |
| review → build | 39 | Review finds issues, build fixes (good) |
| build → review | 42 | Build followed by quality gate (good) |
| verify → ship | 40 | Standard ship path (good) |
| ship → build | 23 | Ship failed/denied, back to build |

---

## Five Problems (Evidence-Based)

### 1. Build → build chains (143 transitions, 21% of all routing)

The supervisor enters a reactive loop without quality gates. In huli, 71% of builds had no review after them. Worst case: huli session `6a357538` — 70 gate skips in 144 dispatches.

### 2. Ship gets denied more than it succeeds

Huli: 69% DENIED. Crucible-pro: 57% DENIED. The supervisor tries to ship before the operator is ready. In long sessions, this happens because the operator keeps expanding scope but the supervisor doesn't track that.

### 3. Mega-sessions destroy efficiency

Sessions over ~30 dispatches lose coherence. Huli `6a357538`: 144 dispatches, 51% waste. Crucible-pro `6035011f`: 66 dispatches, 24% waste. Both should have been multiple separate sessions.

### 4. Supervisor doesn't catch architectural problems

Huli `e38b7e31`: supervisor built tools with raw sqlc instead of handler services. The operator had to come back with a full refactor spec. The supervisor executes what it's told — it doesn't evaluate whether the approach is architecturally right.

### 5. Clean pipeline sessions work well

When scope is bounded and the supervisor follows the pipeline:
- huli `cc2b8e47`: 9 dispatches, 0 waste, 0 gate skips, 0 interventions, PR shipped
- adk-go `cae4e4eb`: 5 dispatches, 0 waste, review found missing tests, supervisor fixed them, PR shipped
- crucible-pro `ad4f9f3c`: 8 dispatches, 0 waste, full pipeline, PR #33 shipped

---

## Controlled Eval Tasks

### Task 1: #47 — Station Isolation Reminder (Bug Fix, Size S)

**Input:** `"new work order gh#47"`
**Session:** `d2816820`
**PR:** https://github.com/dmora/crucible-pro/pull/71

| Metric | Value |
|---|---|
| Operator messages | 1 |
| Corrections | 0 |
| Station dispatches | 9 |
| Actual route | draft → inspect → draft → inspect → build → build → review → verify → ship |
| Prompt references | 1/9 |
| Wall clock | ~33 min |
| Outcome | **PR shipped, zero intervention** |

Supervisor self-corrected twice (wrong plan path, wrong file location). Review passed. Verify wrote a unit test. PR is correct, minimal (+26 -2), tested.

**Gaps:** Footer says what NOT to do but doesn't show what TO do. Won't fully fix the context-passing problem (#70).

### Task 2: #68 — Pipeline Circuit Breaker (Infrastructure, Size S)

**Input:** `"new work order gh#68"`
**Session:** `6093c6db`
**PR:** https://github.com/dmora/crucible-pro/pull/73

| Metric | Value |
|---|---|
| Operator messages | 1 |
| Corrections | 0 |
| Station dispatches | 8 |
| Actual route | draft → inspect → inspect → build → review → build → verify → ship |
| Prompt references | 0/8 |
| Wall clock | ~27 min |
| Outcome | **PR shipped, zero intervention** |

Review caught 3 critical issues (race conditions, nil panic). Build fixed them. +582 lines (205 implementation + 357 tests).

**Gaps found by external reviewers (not by Crucible):**
- **Codex P1:** Steering plugin's `afterTool` runs before circuit breaker in plugin chain. ADK short-circuits on error → breaker never sees failures when steering is active. **Breaker is dead in most sessions.**
- **Codex P2:** Pending flag lost across runner lifecycles.
- Soft hint (system instruction) not hard gate — supervisor can ignore it.

### Task 3: #8 — Role Seal (Aborted)

Pipeline built a technically sound MCP tool filter for a misdiagnosed problem. The supervisor doesn't have file editing tools — the issue conflated MCP leakage with coder bias. **The pipeline can execute a plan perfectly when the plan itself is wrong.**

### Tasks 4-5: #67, #69 — Closed

- #67 (timeout): Wrong approach — fixed timeouts would kill legitimate long-running work
- #69 (structured steering): Unnecessary — LLMs already parse natural language well. The problem is the supervisor ignoring steering, not the format.

---

## Post-Crucible Review (Operator Command Center)

The operator maintains long-running Claude Code sessions where they merge Crucible's PRs, test the running app, and find what Crucible missed. This is the actual outer quality gate.

### What Crucible's stations catch

- Code-level bugs (duplicate branches, race conditions, missing tests)
- Compilation failures
- Unit test regressions

### What Crucible's stations miss (found by operator + external reviewers)

| PR | What was missed | How it was caught |
|---|---|---|
| #34 (issue #7) | `AddFinish` dropped usage tokens when ErrorCode overrode FinishReason — silent data loss | Codex external reviewer |
| #33 (issue #3) | Duplicate if branches for turnInput/initialPrompt | Gemini Code Assist |
| Boundary filters (743b7fb4) | Components built but never wired end-to-end — interpreter, result filter, dispatch enricher all existed in isolation | Operator tested the running app |
| Station scope dialog | `loadedPaths` array never refreshed after config write — scope changes didn't persist | Operator tested the running app |
| #73 (issue #68) | Plugin ordering bug — breaker dead when steering active | Codex external reviewer |

### The pattern

Crucible's review station does static code analysis — it reads the diff and checks it. It does NOT:
- Run the application and test cross-component behavior
- Check plugin ordering and runtime interaction effects
- Verify that separately-built components are wired together end-to-end
- Catch silent data loss in metadata paths

**The real pipeline is:**

```
Crucible: draft → inspect → build → review → verify → ship (PR)
    ↓
Operator: merge → test live → find integration bugs → fix or new work order
    ↓
External reviewers: Codex, Gemini Code Assist catch what both missed
```

The operator is the integration test layer. External AI reviewers catch code-level bugs the review station misses due to different model perspectives.

---

## Key Takeaways

1. **Stations work.** Build 94-96%, verify 100%, review catches real bugs when it runs.
2. **The supervisor is the weak link.** Skips review 52% of the time, takes shortcuts (build→build), tries to ship before ready.
3. **Mega-sessions are toxic.** 51% waste above 30 dispatches. Bounded scope = clean runs.
4. **The operator is the real quality gate.** 18% correction rate during sessions + post-merge testing catches integration bugs stations can't see.
5. **External reviewers add value.** Codex caught a P1 bug (plugin ordering) that Crucible's review station, Gemini, and manual review all missed.
6. **The pipeline executes plans well but doesn't validate problem diagnosis.** Task #8 showed the pipeline building the wrong solution to a misdiagnosed problem.
