# Station Boundaries

A station is an agentrun process (Claude Code, Codex, OpenCode, or any supported CLI backend) scoped to one role. This document defines what a single station can and cannot do reliably, based on real-world evidence — not theory.

## What a Station Solves

Turning structured intent into working code changes, within one codebase, within one session.

## Reliability Tiers

### Works Almost Every Time

- **Boilerplate and repetitive transforms** — schema files, config translations, migrations across dozens of files. Same pattern applied N times.
- **Greenfield builds** — new modules, new endpoints, new components where there's no legacy context to navigate.
- **Debugging with test coverage** — tests provide the verification loop. Station runs tests, sees failure, fixes, repeats.
- **Codebase exploration** — reading, searching, understanding. No side effects, no risk.
- **Large-scale migrations** — mechanical transforms at scale. COBOL to Java, framework version upgrades, API contract changes.
- **Throwaway scripts and tooling** — low quality bar, disposable output, quick verification.

Common thread: the output is **mechanically verifiable** (tests pass, it compiles, the migration runs) or **low-stakes** (disposable, prototype).

### Works With Supervision

- **Feature implementation in known codebases** — misses project conventions, produces verbose output. Needs CLAUDE.md + plan mode + review.
- **Large refactors** — drifts from intent as context fills. Needs test suite as guardrail.
- **Multi-file coordinated changes** — loses consistency across files. Needs careful scoping.

Common thread: requires **judgment that tests can't encode**. A reviewer must check the output.

### Unreliable

- **Large inconsistent legacy codebases** — can't fit enough context. Lacks architectural intuition built over months of working in the code.
- **Unsupervised long sessions** — errors compound. Station sometimes "fixes" tests to pass rather than fixing the underlying bug.
- **Production quality at scale** — output is verbose (often 2x what's needed), covers every edge case no matter how unlikely. Requires trimming.
- **Recovery after wrong direction** — once off-track, sunk context makes recovery harder than restarting.
- **Tasks where verification is harder than creation** — if the reviewer can't tell whether the output is correct, supervision fails.

Common thread: the task exceeds what **one session with one context window** can hold, or correctness requires judgment the station doesn't have.

## Measured Reliability (692 dispatches, 28 sessions, 3 projects)

| Station | Dispatches | Reliability | Notes |
|---|---|---|---|
| verify | 73 | 98-100% | Most reliable. Runs tests, reports results. |
| build | 279 | 94-96% | High technical success. Output quality varies — verbose, sometimes wrong location. |
| review | 55 | 78-95% | Catches real bugs (race conditions, nil panics, missing tests) when it runs. |
| inspect | 91 | 83-90% | Occasionally returns empty or crashes (exit status 1). |
| draft | 108 | 83-86% | Sometimes returns empty or doesn't save plan file path. |
| design | 14 | 67-100% | Small sample. Occasionally returns empty on exploratory tasks. |
| ship | 72 | 26-38% | Almost all failures are operator DENIED, not station failures. |

### What Stations Catch

- Compilation failures and test regressions (verify)
- Race conditions, nil panics, missing unit tests (review)
- SDK type mismatches, missing error handling (inspect on draft plans)

### What Stations Miss

Found by operator testing or external AI reviewers, not by stations:

- Components built but never wired end-to-end (boundary filters existed in isolation)
- Config state not refreshing after writes (loadedPaths bug)
- Token metadata silently dropped during event replay (PR #34)
- Plugin ordering bugs causing feature to be dead at runtime (PR #73)

Common thread: stations do **static code analysis**. They read the diff. They don't run the application or test cross-component integration.

## Hard Limits

| Boundary | Limit |
|---|---|
| Scope | One repo, one task, one session |
| Context | What fits in the context window. Selective reading, not full codebase understanding |
| Quality | "It compiles and tests pass" — not "it's wired correctly" or "it works end-to-end" |
| Time | Errors compound after ~30 min of active work. Context exhaustion degrades quality |
| Memory | No learning across sessions. Same mistakes can recur indefinitely |
| Decisions | Tactical only (which file, which approach). Not strategic (what to build, when to stop, where to route) |
| Integration | Cannot test cross-component wiring, runtime behavior, or plugin interaction effects |

## Sources

- Anthropic internal dogfooding study (132 engineers, Aug 2025)
- METR randomized controlled trial (16 developers, 246 issues)
- Anthropic 2026 Agentic Coding Trends Report
- Spotify migration case study (650+ AI changes/month)
- Crucible eval: 692 dispatches, 28 sessions across crucible-pro, adk-go, huli (March 2026)
