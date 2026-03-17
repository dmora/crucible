---
description: Dual SQLite architecture, goose migrations, sqlc queries, session persistence patterns
paths: ["internal/db/**", "internal/session/**"]
---

# Database Layer Patterns

## Dual SQLite Architecture

Crucible uses two separate SQLite databases to avoid table name collisions:

| Database | Driver | ORM/Tool | Purpose |
|----------|--------|----------|---------|
| `crucible.db` | `ncruces/go-sqlite3` (CGo-free wasm) | goose migrations + sqlc | Crucible session metadata (title, tokens, cost, todos), file history, stats |
| `crucible-adk.db` | `glebarez/sqlite` (CGo-free GORM) | GORM auto-migrate | ADK conversation data (sessions, events, app/user state) |

**Why separate?** ADK hardcodes `TableName() = "sessions"` which collides with Crucible's goose `sessions` table.

## Migration Workflow (goose)

Migrations live in `internal/db/migrations/`. Numbered sequentially (`00001_`, `00002_`, etc.).

```bash
# Migrations are applied automatically at startup via goose.Up()
# To add a new migration:
# 1. Create internal/db/migrations/NNNNN_description.sql
# 2. Write -- +goose Up and -- +goose Down sections
# 3. Run `go build` and test
```

## sqlc Workflow

Queries defined in `internal/db/queries/`. Generated Go code in `internal/db/`.

```bash
# After modifying queries:
sqlc generate
```

## Session Persistence

- **`session.Service`** — reads/writes Crucible session metadata (title, tokens, cost) in `crucible.db`
- **`message.Service`** — read-only interface backed by ADK events (no direct DB writes)
- **Session lifecycle:**
  - Create: Crucible creates in `crucible.db`, agent creates ADK session in `crucible-adk.db`
  - Message: ADK auto-persists events; agent publishes to `pubsub.Broker` for UI
  - Reload: `adkMessageService.List()` → `eventsToMessages()` → UI
  - Delete: Crucible deletes from `crucible.db`, `WithOnDelete` callback deletes from `crucible-adk.db`

## Token Accounting

- `UpdateSessionUsage()` uses additive updates for station tokens (`total_tokens = total_tokens + ?`, `station_tokens` JSON merge) and absolute overwrites for supervisor tokens
- `station_tokens` column stores JSON map: `{"build": {"input": N, "output": N, ...}, ...}`
- `UsageLedger` (in `internal/agent/`) accumulates deltas in-memory, flushed to DB periodically
