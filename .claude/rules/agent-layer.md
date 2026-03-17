---
description: ADK agent layer patterns — event processing, station tools, pubsub, system prompt
paths: ["internal/agent/**"]
---

# Agent Layer Patterns

## Architecture

The agent layer bridges Google ADK and the TUI via pubsub:

```
llmagent.New() → runner.New() → runner.Run() event iterator
  → processEvent() → pubsub.Broker[message.Message] → UI
```

## Key Files

| File | Responsibility |
|------|---------------|
| `agent.go` | Core loop: `ensureADKSession()` → ADK runner → `processEvent()` → publishes messages |
| `agentrun.go` | `processManager` (per-session process pool) + `newStationTool()` per station |
| `coordinator.go` | Model construction (`gemini.NewModel()`), provider resolution, hold flag |
| `event_convert.go` | ADK `session.Events` ↔ `message.Message` conversion for session reload |
| `message_service.go` | Read-only `message.Service` backed by ADK events (no direct DB writes) |
| `steering_plugin.go` | Post-station routing hints (one-turn ephemeral injection into SystemInstruction) |
| `gate.go` | Per-station gate enforcement via `permission.Service.Request()` |
| `process_state.go` | `ProcessInfo`, `ProcessActivity`, `ProcessPhase`, `OperatorState` |
| `usage_ledger.go` | Per-session token/cost accounting (thread-safe via mutex) |
| `loop_detection.go` | Detects tool call loops (consecutive identical calls or total cap) |
| `askuser_tool.go` | Structured operator questions via `askuser.Service` |

## Station Tool Pattern

Each station is an ADK `functiontool.New[stationInput, stationResult]()` wrapping agentrun:

1. `newStationTool(name)` creates the function tool with `processManager` closure
2. LLM calls tool with `{ task: string }`
3. `buildTask()` wraps task with skill prefix on first turn (Claude: `"Load your <skill> skill and then: <task>"`)
4. agentrun spawns/resumes Claude Code process
5. Callback streams `ProcessInfo` → pubsub → UI station cards
6. `runWithRecovery()` handles context exhaustion (max 3 replacements)
7. Result returned to supervisor for interpretation

## ADK System Prompt

ADK adds **zero** built-in system prompt — purely passthrough. Crucible's `coder.md.tpl` (Go text/template, `//go:embed`) builds the system prompt once at startup. Tools use Gemini's native `FunctionDeclarations`, NOT injected into prompt.

## Concurrency Patterns

- `processManager` — one per session, goroutine-safe via internal locking
- `usageLedgers` — `csync.Map[string, *UsageLedger]` keyed by session ID
- `processStates` — `csync.Map` for station process info, published via dedicated pubsub broker
- `steeringStore` — mutex-guarded push/drain queue (one-turn ephemeral)
- ADK runner events processed in single goroutine, publishes to broker for fan-out
