Analyze this project and create or update **{{.Config.Options.InitializeAs}}** — a context file that documents everything an AI agent needs to work effectively in this repository.

If **{{.Config.Options.InitializeAs}}** already exists, read it first and improve it rather than starting from scratch. Also check for existing rule files (`.cursorrules`, `.github/copilot-instructions.md`, `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`) — if any exist, incorporate their useful context into {{.Config.Options.InitializeAs}}.

The file should cover:
- Build, test, lint, and run commands
- Code organization and directory structure
- Naming conventions and style patterns
- Testing approach and patterns
- Important gotchas or non-obvious project-specific details

Base the content entirely on what you observe in the codebase. Do not invent commands, conventions, or patterns that aren't actually present.
