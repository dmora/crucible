---
description: Crucible project identity, architecture concepts, and terminology
---

# Crucible — Project Overview

Crucible is an **autonomous software development orchestrator**. A **Supervisor LLM** (Gemini via Google ADK) talks to the user and delegates to **stations** — external agent CLIs (Claude Code) that do the actual work.

## Core Concepts

- **Supervisor** — Gemini via ADK. Role-sealed: delegates to stations, never writes code directly.
- **Stations** — External agent CLIs spawned via agentrun. Disposable workers (can exhaust context, crash, be canceled). Seven stations: `design`, `plan`, `build`, `inspect`, `review`, `verify`, `ship`.
- **agentrun** — Go library (`github.com/dmora/agentrun`) wrapping agent CLIs with uniform Engine/Process/Message model. Wrapped as an ADK `functiontool`.
- **Observation deck** — The TUI. Primary UX is observing station activity via station cards (semantic states, activity trees, verdicts, fuel gauges).

## How It Works

```
USER → SUPERVISOR (Gemini/ADK)
         ├─ Responds directly (chat, reasoning)
         ├─ Google Search (grounded answers)
         └─ Delegates to stations via tool calls
              └─ Station streams activity → UI renders live → returns result
```

## Station Configuration

| Station | Mode | Skill | Purpose |
|---------|------|-------|---------|
| `design` | — | `claude-foundry:design` | Architecture analysis |
| `plan` | plan | — | Technical spec, read-only |
| `build` | act | `feature-dev:feature-dev` | Implementation, full write |
| `inspect` | plan | `claude-code-quality:review-plan` | Plan validation / QC |
| `review` | plan | `claude-code-quality:rigorous-pr-review` | Structured code review |
| `verify` | act | — | Execution-based validation, runs tests |
| `ship` | act (gated) | — | Package changes into a PR |

Adding stations is config-only — add to `config.DefaultStations`, UI picks it up via `RegisterStationNames()`.

## Visual Design

Aesthetic inspired by Alien, Blade Runner, Pacific Rim. Monochrome/limited palette (amber/green/cyan on dark), ALL-CAPS labels, grid layouts, box-drawing borders. Industrial nomenclature: "STATION", "SECTOR", "PIPELINE", "MANIFOLD".

## Key Design Decisions

1. **ADK is the permanent agent framework** — powers LLM interactions, tools, session management.
2. **Multi-agent via agentrun tool** — all stations use Claude Code as external CLI. agentrun spawns long-lived streaming subprocesses.
3. **Supervisor delegates, doesn't code** — role-sealed. Reasons and routes, never edits files.
4. **Dual SQLite persistence** — `crucible.db` (goose/sqlc) for Crucible metadata; `crucible-adk.db` (GORM) for ADK conversations.
5. **TUI is the observation deck** — station cards are the information radiators, chat is the interaction surface.
6. **Stations are disposable, supervisor is continuity** — supervisor retries, reframes, routes work across station failures.

## External References

- **adk-tui** (`/Users/davidmora/Projects/github.com/dmora/adk-tui`) — sibling TUI project, reference for Bubble Tea v2 patterns, render cache, panel system, powerline.
- **agentrun** (`/Users/davidmora/Projects/github.com/dmora/agentrun`) — agent CLI abstraction library.
- **adk-go-extras** (`/Users/davidmora/Projects/github.com/dmora/adk-go-extras`) — reusable ADK extensions (artifact persistence, notifications).
- **Issues:** [crucible-pro issues](https://github.com/dmora/crucible-pro/issues) (private — all issues live here)
- **GitHub Project #5** — 2-week iterations, fields: Status/Priority/Size/Iteration.
