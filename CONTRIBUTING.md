# Contributing to Crucible

## Development Setup

```bash
git clone https://github.com/dmora/crucible.git
cd crucible
go build -o bin/crucible .
go test ./...
```

Requirements:
- Go 1.26+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2.9+

## Pull Request Process

1. Create a feature branch from `main`
2. Make your changes
3. Run `go test -race ./...` and `go vet ./...`
4. Open a PR against `main`

All PRs require CI to pass before merge.

## Project Conventions

Architecture and layer-specific patterns are documented in `.claude/rules/`:

- `project-overview.md` — mission, concepts, terminology
- `agent-layer.md` — ADK patterns, station tools, pubsub
- `ui-layer.md` — Bubble Tea, ultraviolet, styling, station cards
- `db-layer.md` — Dual SQLite, goose migrations, sqlc

## Station Development

Adding a new station is config-only. Add an entry to `DefaultStations` in `internal/config/config.go` and the UI picks it up automatically via `RegisterStationNames()`. See the existing stations for the field reference.
