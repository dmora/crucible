# Crucible — Project Context

Crucible is a terminal application that orchestrates autonomous software development. It is built with Go.

This is Crucible's own repository — you are working on yourself.

## Module

`github.com/dmora/crucible`

## Build & Test

```bash
go build -o bin/crucible .
go test ./...
go vet ./...
```

## Project Layout

- `cmd/` — CLI entry point
- `internal/agent/` — Core orchestration (coordinator, station tools, prompts)
- `internal/ui/` — TUI (Bubble Tea model, chat rendering, dialogs, styles)
- `internal/config/` — Configuration and station setup
- `internal/session/` — Session metadata
- `internal/message/` — Message types and service interface
- `internal/db/` — SQLite persistence (goose migrations)
- `internal/pubsub/` — Generic pub/sub broker

## Key Conventions

- Dual SQLite: `crucible.db` for app metadata, `crucible-adk.db` for session history
- Monochrome palette, monospace, factory nomenclature
- Station configuration lives in `internal/config/config.go`
