# CRUCIBLE

Autonomous software development orchestrator. Supervisor LLM (Gemini/ADK) delegates to stations (Claude Code via agentrun). See `.claude/rules/` for layer-specific patterns.

## Module & Build

```
module: github.com/dmora/crucible
```

```bash
go build -o bin/crucible .
go test ./...
go vet ./...
```

## Directory Layout

```
cmd/                        # CLI entry point (cobra)
internal/
├── agent/                  # ADK agent layer — see .claude/rules/agent-layer.md
├── ui/                     # Bubble Tea v2 TUI — see .claude/rules/ui-layer.md
│   ├── model/              # Main UI model
│   ├── chat/               # Chat viewport + station cards
│   ├── styles/             # Theming (5 palettes, 4-tier derivation)
│   └── dialog/             # Modals, overlays, permissions
├── session/                # Session metadata (title, cost, tokens)
├── message/                # Message types, read-only Service interface
├── db/                     # SQLite persistence — see .claude/rules/db-layer.md
├── config/                 # Models, providers, stations, workflow
├── askuser/                # Blocking request/response for operator questions
└── pubsub/                 # Generic pub/sub broker
```

## Stations

| Station | Mode | Skill | Purpose |
|---------|------|-------|---------|
| `design` | — | `claude-foundry:design` | Architecture analysis |
| `plan` | plan | — | Technical spec, read-only |
| `build` | act | `feature-dev:feature-dev` | Implementation |
| `inspect` | plan | `claude-code-quality:review-plan` | Plan validation |
| `review` | plan | `claude-code-quality:rigorous-pr-review` | Code review |
| `verify` | act | — | Execution-based validation, runs tests |
| `ship` | act (gated) | — | Package changes into a PR |

Config-only to add stations: `config.DefaultStations` + `RegisterStationNames()`.

## Persistence

| Database | Driver | Tool | Contents |
|----------|--------|------|----------|
| `crucible.db` | `ncruces/go-sqlite3` | goose + sqlc | Session metadata, tokens, cost, history |
| `crucible-adk.db` | `glebarez/sqlite` | GORM | ADK sessions, events, state |

Separate DBs because ADK hardcodes `TableName() = "sessions"`.

## Config Locations

- `~/.config/crucible/crucible.json` — static user prefs
- `~/.local/share/crucible/crucible.json` — runtime state (models, theme, reasoning)
- `.crucible/` — databases, logs

## Project Management

- **Issues:** [crucible-pro issues](https://github.com/dmora/crucible-pro/issues) (private)
- **Project board:** [GitHub Project #5](https://github.com/users/dmora/projects/5) — 2-week iterations, Priority P0/P1/P2, Size XS–XL.

## Context Rules

Layer-specific guidance is in `.claude/rules/`:
- `project-overview.md` — mission, concepts, terminology, design decisions
- `agent-layer.md` — ADK patterns, station tools, pubsub, system prompt (`internal/agent/**`)
- `ui-layer.md` — Bubble Tea, ultraviolet, styling, station cards (`internal/ui/**`)
- `db-layer.md` — Dual SQLite, goose migrations, sqlc, session persistence (`internal/db/**`, `internal/session/**`)
