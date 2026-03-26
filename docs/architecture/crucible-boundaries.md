# Crucible Boundaries

Based on 692 station dispatches across 28 sessions, 3 projects (crucible-pro, adk-go, huli), and post-Crucible operator review sessions. March 2026.

## What Crucible Is

One agent (Gemini supervisor) with station tools. Each station is an agentrun process — Claude Code, Codex, OpenCode, or any supported CLI backend. The operator controls what enters the line. The supervisor routes work through stations sequentially. One workpiece at a time.

## What It Does Well

**Bounded tasks with clear issues ship cleanly.** Sessions with <15 dispatches on a single well-defined issue produce 0% waste, full pipeline compliance, and merged PRs. Examples: cc2b8e47 (9 dispatches, 0 waste), cae4e4eb (5 dispatches, 0 waste), ad4f9f3c (8 dispatches, 0 waste).

**Draft ↔ inspect forces planning discipline.** 67 draft→inspect transitions across all sessions. Inspect catches real problems before build starts — SDK type mismatches, missing error handling, architectural concerns. 2-3 rounds is typical before the plan is approved.

**Review catches code-level bugs.** Race conditions, nil panics, missing unit tests, unsafe type conversions. When review runs, it finds real issues and the supervisor routes back to build to fix them.

**Autonomous operation works on simple tasks.** Both controlled eval tasks (#47, #68) shipped PRs with zero operator intervention. The supervisor self-corrected when inspect rejected plans or build put code in the wrong place.

## Where It Breaks

**Review gets skipped 52% of the time.** The supervisor enters build→build chains (143 transitions, 21% of all routing) without sending work through quality gates. Under operator pressure or in long sessions, the supervisor takes shortcuts.

**Sessions degrade after ~30 dispatches.** Huli session 6a357538: 144 dispatches, 51% waste. Crucible-pro 6035011f: 66 dispatches, 24% waste. The supervisor loses coherence and falls into reactive loops. What should be multiple work orders gets jammed into one session.

**Ship doesn't know when the operator is ready.** 60%+ of ship attempts are DENIED. The supervisor can tell when code compiles and tests pass. It cannot tell when the operator considers the work complete. In long sessions with expanding scope, the supervisor repeatedly tries to ship mid-iteration.

**Cannot diagnose problems — only execute plans.** Eval task #8: the pipeline built a technically sound MCP tool filter for a misdiagnosed root cause. Draft and inspect validated the plan's implementation quality but never questioned whether it solved the right problem.

**Cannot catch architectural mistakes.** Huli e38b7e31: supervisor built tools with raw sqlc when the project uses handler services. It never raised the concern. The operator had to come back with a full refactor specification.

**Cannot catch integration-level bugs.** Crucible's review does static code analysis. It reads the diff. It does not run the application, test cross-component wiring, or verify that separately-built pieces connect. Components built but never registered in plugin chains (boundary filters session). Config state not refreshing after writes (station scope bug). Token metadata silently dropped (PR #34). All caught by the operator or external reviewers, not by Crucible.

## The Real Pipeline

```
Crucible: draft → inspect → build → review → verify → ship (PR)
    ↓
Operator: merge → test live → find integration bugs → fix or new work order
    ↓
External reviewers: catch code-level bugs from a different model perspective
```

The operator is the integration test layer. External AI reviewers (Codex, Gemini Code Assist) catch code-level bugs the review station misses. Crucible produces draft-quality PRs, not production-ready code.

## Hard Limits

| Limit | Evidence |
|---|---|
| One workpiece at a time | Sequential by design |
| ~30 dispatches before coherence degrades | 51% waste above this threshold |
| Cannot enforce its own quality gates | 52% gate skip rate |
| Cannot validate problem diagnosis | Task #8 built wrong solution |
| Cannot catch integration bugs | Static analysis only, doesn't run the app |
| Cannot know when operator is ready to ship | 60%+ ship DENIED rate |
| 18% operator correction rate | Operator compensates for supervisor mistakes |
| External reviewers find bugs stations miss | Plugin ordering (Codex P1), token loss (Codex), duplicate branches (Gemini) |

## When It's Worth It

- Task is Size S-M with a clear GitHub issue
- The operator provides a work order and reviews the PR after
- The project has test coverage (verify station needs tests to run)
- The codebase is well-structured (stations need conventions to follow)

## When It's Overhead

- Quick fix a single Claude Code session handles in 2 minutes
- Exploratory/research work with no clear deliverable
- Tasks requiring deep architectural judgment
- Sessions that would exceed ~30 dispatches (split into multiple work orders)

## Sources

- `docs/eval/results.md` — full eval data
- `docs/architecture/station-boundaries.md` — Claude Code session limits
- 692 dispatches, 28 sessions, 3 projects, 5 days of operation
