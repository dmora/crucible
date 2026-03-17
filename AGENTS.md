# Crucible Development Guide

## Build/Test/Lint Commands

- **Build**: `go build .` or `go build -o bin/crucible .`
- **Test**: `go test ./...` (run single test: `go test ./internal/config/... -run TestDefaultStations`)
- **Update Golden Files**: `go test ./... -update` (regenerates .golden files when test output changes)
  - Update specific package: `go test ./internal/ui/chat/... -update`
- **Lint**: `golangci-lint run ./...`
- **Format**: `gofumpt -w .`
- **Vet**: `go vet ./...`

## Code Style Guidelines

- **Imports**: Use `goimports` formatting, group stdlib, external, internal packages.
- **Formatting**: Use gofumpt (stricter than gofmt), enabled in golangci-lint.
- **Naming**: Standard Go conventions - PascalCase for exported, camelCase for unexported.
- **Types**: Prefer explicit types, use type aliases for clarity (e.g., `type AgentName string`).
- **Error handling**: Return errors explicitly, use `fmt.Errorf` for wrapping.
- **Context**: Always pass `context.Context` as first parameter for operations.
- **Interfaces**: Define interfaces in consuming packages, keep them small and focused.
- **Structs**: Use struct embedding for composition, group related fields.
- **Constants**: Use typed constants with iota for enums, group in const blocks.
- **Testing**: Use testify's `require` package, parallel tests with `t.Parallel()`,
  `t.SetEnv()` to set environment variables. Always use `t.TempDir()` when in
  need of a temporary directory. This directory does not need to be removed.
- **JSON tags**: Use snake_case for JSON field names.
- **File permissions**: Use octal notation (0o755, 0o644) for file permissions.
- **Log messages**: Log messages must start with a capital letter (e.g., "Failed to save session" not "failed to save session").
- **Comments**: End comments in periods unless comments are at the end of the line.

## Formatting

- ALWAYS format any Go code you write.
  - First, try `gofumpt -w .`.
  - If `gofumpt` is not available, use `goimports`.
  - If `goimports` is not available, use `gofmt`.

## Comments

- Comments that live on their own lines should start with capital letters and
  end with periods. Wrap comments at 78 columns.

## Committing

- ALWAYS use semantic commits (`fix:`, `feat:`, `chore:`, `refactor:`, `docs:`, `sec:`, etc).
- Try to keep commits to one line, not including your attribution. Only use
  multi-line commits when additional context is truly necessary.

## Working on the TUI (UI)

Anytime you need to work on the TUI, before starting work read the `internal/ui/AGENTS.md` file.
