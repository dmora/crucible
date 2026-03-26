# Crucible Pro

Private build. Public upstream: `dmora/crucible`.

## Build

```
module: github.com/dmora/crucible
```

```bash
go build -o bin/crucible-pro .
go test ./...
go vet ./...
```

## Structure

```
cmd/                        # CLI entry point (cobra)
internal/
├── agent/                  # Core logic
├── ui/                     # Terminal UI (Bubble Tea v2)
│   ├── model/              # Main UI model
│   ├── chat/               # Chat viewport
│   ├── styles/             # Theming (5 palettes)
│   └── dialog/             # Modals, overlays
├── session/                # Session metadata (title, cost, tokens)
├── message/                # Message types
├── db/                     # SQLite persistence
├── config/                 # Models, providers, tools, workflow
├── askuser/                # Blocking request/response for user questions
└── pubsub/                 # Generic pub/sub broker
```

## Persistence

| Database | Driver | Contents |
|----------|--------|----------|
| `crucible.db` | ncruces/go-sqlite3, goose + sqlc | Session metadata, tokens, cost |
| `crucible-adk.db` | glebarez/sqlite, GORM | Conversations, events, state |

## Config

- `~/.config/crucible/crucible.json` — user prefs
- `~/.local/share/crucible/crucible.json` — runtime state
- `.crucible/` — databases, logs

## Repo

- Go module path: `github.com/dmora/crucible` (do not change — enables merge with public repo)
- Issues: [crucible-pro issues](https://github.com/dmora/crucible-pro/issues)
- Project board: [GitHub Project #5](https://github.com/users/dmora/projects/5)
