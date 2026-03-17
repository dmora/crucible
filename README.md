# CRUCIBLE

**Autonomous software development orchestrator.**

Agentic AI drifts from intent during autonomous execution. Crucible addresses this by separating conversation from execution -- a supervisor stays in dialogue with the operator while disposable stations do the work. The supervisor reasons, verifies, and routes; stations read, write, build, and review. Alignment is maintained through conversation, not guardrails.

Built on [conversation-first alignment](https://github.com/dmora/conversation-first-alignment): the idea that alignment isn't a safety layer bolted on top, but an emergent property of ongoing dialogue between human and AI.

```
OPERATOR
  |
  v
SUPERVISOR (Gemini via ADK)
  |-- Responds directly (chat, reasoning)
  |-- Google Search (grounded answers)
  +-- Delegates to stations via tool calls
       |
       |-- design   Architecture analysis, solution design
       |-- draft    Technical spec, read-only
       |-- build    Implementation, full write access
       |-- inspect  Plan validation and QC
       |-- review   Structured code review, read-only
       |-- verify   Execution-based validation
       +-- ship     Package changes into a PR (gated)
              |
              v
       Station streams activity --> UI renders live
       Station returns result  --> Supervisor interprets
```

The supervisor is role-sealed: it delegates work to stations but does not write code itself. Stations are disposable workers -- the supervisor is the continuity layer.

## INSTALLATION

### Homebrew

```bash
brew install dmora/tap/crucible
```

### Download binary

Precompiled binaries for Linux, macOS, and FreeBSD (amd64/arm64) are available on the [releases page](https://github.com/dmora/crucible/releases).

### From source

```bash
go install github.com/dmora/crucible@latest
```

### Build from repo

```bash
git clone https://github.com/dmora/crucible.git
cd crucible
go build -o bin/crucible .
```

**Prerequisites:**

- Google Cloud credentials (Vertex AI with ADC) or `GEMINI_API_KEY`
- A station backend CLI installed and authenticated (default config uses Claude Code)

## QUICKSTART

**1. Set credentials**

For Vertex AI (recommended):

```bash
gcloud auth application-default login
export GOOGLE_CLOUD_PROJECT=your-project
export GOOGLE_CLOUD_LOCATION=us-central1  # optional, defaults to us-central1
```

For Gemini API:

```bash
export GEMINI_API_KEY=your-key
```

**2. Run Crucible in a project directory**

```bash
cd your-project
crucible
```

**3. What happens**

The supervisor loads and station cards appear in the sidebar. Type a request in the chat input. The supervisor reasons about the work, delegates to the appropriate station, and you observe the activity streaming live. When a station completes, the supervisor interprets the result and routes to the next station or reports back to you.

```
You type a request
  --> Supervisor reasons about the task
    --> Delegates to station (e.g. design, build)
      --> Station streams activity (you watch live)
    --> Supervisor interprets result
  --> Routes to next station or reports completion
```

## COMMANDS

```
crucible [command] [--flags]
```

| Command | Description |
|---------|-------------|
| *(default)* | Run in interactive TUI mode |
| `run [prompt...]` | Run a single non-interactive prompt and exit |
| `dirs [config\|data]` | Print directories used by Crucible |
| `logs` | View crucible logs |
| `models` | List all available models from configured providers |
| `projects` | List project directories |
| `schema` | Generate JSON schema for the configuration file |
| `stats` | Show usage statistics |
| `update-providers [url]` | Update provider metadata from the model catalog |
| `completion [shell]` | Generate shell autocompletion script |

### Global flags

| Flag | Description |
|------|-------------|
| `-c, --cwd` | Set working directory |
| `-D, --data-dir` | Custom crucible data directory |
| `-d, --debug` | Enable debug logging |
| `-y, --yolo` | Auto-accept all permissions (dangerous) |
| `--worktree` | Enable git worktree isolation per session |
| `-v, --version` | Print version |
| `-h, --help` | Help |

### `run` flags

| Flag | Description |
|------|-------------|
| `-m, --model` | Model to use (`model` or `provider/model`) |
| `--small-model` | Small model override |
| `-q, --quiet` | Hide spinner |
| `-v, --verbose` | Show logs |

### `logs` flags

| Flag | Description |
|------|-------------|
| `-f, --follow` | Follow log output |
| `-t, --tail` | Show last N lines (default: 1000) |

### Examples

```bash
crucible                                              # interactive TUI
crucible -d                                           # debug mode
crucible -d -c /path/to/project                       # debug in specific directory
crucible run "Explain the use of context in Go"       # non-interactive prompt
curl https://example.com | crucible run "Summarize"   # pipe stdin
crucible run -q "Generate a README"                   # quiet mode
crucible dirs config                                  # print config directory
crucible logs -f                                      # tail logs
crucible schema > crucible-schema.json                # export config schema
```

## KEYBOARD SHORTCUTS

### Global

| Key | Action |
|-----|--------|
| `Ctrl+C` | Quit |
| `Ctrl+G` | Help / more info |
| `Ctrl+P` | Command palette |
| `Ctrl+L` | Model picker |
| `Ctrl+S` | Session picker |
| `Ctrl+Y` | Cycle theme |
| `Ctrl+H` | Hold / gate toggle |
| `Ctrl+A` | Artifacts |
| `Ctrl+E` | Equipment |
| `Ctrl+Z` | Suspend |
| `Tab` | Change focus |

### Editor (input focused)

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Shift+Enter` / `Ctrl+J` | Newline |
| `/` | Add file / commands |
| `@` | Mention file |
| `Ctrl+O` | Open in external editor |
| `Ctrl+F` | Add image |
| `Ctrl+V` | Paste image from clipboard |
| `Ctrl+R+{i}` | Delete attachment at index i |
| `Ctrl+R+R` | Delete all attachments |
| `Up` / `Down` | Message history |

### Chat (viewport focused)

| Key | Action |
|-----|--------|
| `Ctrl+N` | New session |
| `Ctrl+D` | Toggle details |
| `Ctrl+T` | Toggle tasks |
| `Left` / `Right` | Switch section |
| `Up` / `Down` / `j` / `k` | Scroll |
| `Shift+Up` / `Shift+Down` | Scroll one item |
| `f` / `PgDn` | Page down |
| `b` / `PgUp` | Page up |
| `d` / `u` | Half page down / up |
| `g` / `G` | Home / end |
| `c` / `y` | Copy |
| `Space` | Expand / collapse |
| `Esc` | Cancel / clear selection |

## STATIONS

| Station | Mode | Skill | Description |
|---------|------|-------|-------------|
| `design` | -- | `claude-foundry:design` | Architecture analysis, solution design |
| `draft` | plan | -- | Technical spec, no writes |
| `inspect` | plan | `review-plan` | Plan validation and QC verification |
| `build` | act | `feature-dev:feature-dev` | Implementation, full write access |
| `review` | plan | `claude-code-quality:rigorous-pr-review` | Structured code review, read-only |
| `verify` | act | -- | Execution-based validation, runs tests and commands |
| `ship` | act | -- | Package verified changes into a PR (gated) |

Stations are spawned via [agentrun](https://github.com/dmora/agentrun), which supports multiple agent CLI backends (Claude Code, Codex, OpenCode, ACP). The default stations use Claude Code, but any agentrun-compatible backend can be plugged in. Backends and skills are configurable per station. Adding stations is config-only -- the dynamic UI widget registry picks them up automatically.

### Station pipeline

The default workflow follows a pipeline: **design --> draft --> inspect --> build --> review --> verify --> ship**. The supervisor decides which stations to invoke and in what order based on the task. Not every task uses every station -- a simple bug fix might go straight to build, while a complex feature runs the full pipeline.

Gated stations (like `ship`) require operator approval before execution. Toggle gates at runtime with `Ctrl+H`.

## CONFIGURATION

Crucible uses a layered JSON configuration system. Files are loaded in order -- later values override earlier ones. Objects merge recursively.

### Config loading order

```
1. ~/.config/crucible/crucible.json          # global user prefs (static)
2. ~/.local/share/crucible/crucible.json     # runtime state (model, theme — written by TUI)
3. <ancestors>/crucible.json                 # any crucible.json walking up from CWD
4. ./crucible.json                           # project-level config (highest priority)
```

Both `crucible.json` and `.crucible.json` (dot-prefixed) are recognized at each level.

**Env var overrides for config paths:**

| Variable | Overrides |
|----------|-----------|
| `CRUCIBLE_GLOBAL_CONFIG` | Global config directory (`~/.config/crucible/`) |
| `CRUCIBLE_GLOBAL_DATA` | Global data directory (`~/.local/share/crucible/`) |
| `XDG_CONFIG_HOME` | XDG config base |
| `XDG_DATA_HOME` | XDG data base |

### Config schema

Generate the full JSON schema with:

```bash
crucible schema > crucible-schema.json
```

### Providers

Supported types: `gemini` (default), `openai`, `anthropic`, `openai-compat`.

**API key env vars:** `GEMINI_API_KEY` / `GOOGLE_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`.

Add or override providers in config:

```json
{
  "providers": {
    "gemini": {
      "api_key": "$MY_CUSTOM_KEY"
    },
    "my-local-llm": {
      "type": "openai-compat",
      "base_url": "http://localhost:8080/v1",
      "api_key": "not-needed",
      "models": [
        { "id": "llama-3", "name": "Llama 3", "context_window": 128000, "default_max_tokens": 4096 }
      ]
    }
  }
}
```

For Vertex AI, auto-detection reads `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` from the environment. Explicit config:

```json
{
  "providers": {
    "gemini": {
      "backend": "vertex-ai",
      "project": "my-gcp-project",
      "location": "us-central1"
    }
  }
}
```

Provider catalog auto-updates from GitHub (cached with ETag). Disable with `"disable_provider_auto_update": true` in `options`.

### MCP servers

Three transport types are supported:

```json
{
  "mcp": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"],
      "env": { "NODE_ENV": "production" }
    },
    "remote-api": {
      "type": "http",
      "url": "https://api.example.com/mcp/",
      "headers": { "Authorization": "Bearer $API_TOKEN" },
      "timeout": 30
    },
    "streaming": {
      "type": "sse",
      "url": "http://localhost:3000/sse"
    }
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | `stdio` / `http` / `sse` | Transport type (required) |
| `command` | string | Command for stdio servers |
| `args` | string[] | Arguments for stdio servers |
| `url` | string | URL for http/sse servers |
| `headers` | map | HTTP headers (supports `$ENV_VAR` resolution) |
| `env` | map | Environment variables (supports `$ENV_VAR` resolution) |
| `timeout` | int | Timeout in seconds (default: 15) |
| `disabled` | bool | Disable this server |
| `disabled_tools` | string[] | Disable specific tools from this server |

MCP server names must not contain underscores -- use hyphens instead.

### Station customization

Override default stations or add new ones:

```json
{
  "stations": {
    "draft": {
      "backend": "codex",
      "model": "o3"
    },
    "my-custom-station": {
      "backend": "claude",
      "description": "Custom analysis station",
      "skill": "my-namespace:my-skill",
      "options": { "mode": "plan" },
      "gate": true
    }
  }
}
```

User config fully replaces the default for that station name. Stations not overridden keep their defaults. New station names are auto-registered in the UI.

| Field | Type | Description |
|-------|------|-------------|
| `backend` | string | Agent CLI: `claude` (default), `codex`, `opencode`, `opencode-acp` |
| `model` | string | Model ID passed to the agent CLI |
| `description` | string | Tool description the orchestrator sees |
| `skill` | string | Skill identifier (`namespace:name`) |
| `options` | map | Key-value pairs passed to agentrun (e.g. `"mode": "plan"`) |
| `gate` | bool | Require operator approval before invocation |
| `steering` | string | Ephemeral routing hint injected after station returns |
| `artifact_type` | string | Artifact suffix for results (default: `result`) |
| `env` | map | Additional environment variables for the station process |
| `disabled` | bool | Prevent station tool from being registered |

### LSP servers

```json
{
  "lsp": {
    "gopls": {
      "command": "gopls",
      "filetypes": ["go", "mod"],
      "root_markers": ["go.mod"]
    }
  }
}
```

### Context files

Crucible reads project context from markdown files in the working directory. Resolution uses an exclusive fallback chain -- the first tier with any existing file wins:

| Tier | Files | Description |
|------|-------|-------------|
| 1 | `CRUCIBLE.md`, `crucible.md`, `*.local.md` variants | Crucible-specific context |
| 2 | `AGENTS.md` | Cross-tool agent context |
| 3 | `CLAUDE.md`, `claude.md` | Claude-specific context |
| 4 | `.github/copilot-instructions.md`, `.cursorrules`, `.cursor/rules/`, `GEMINI.md`, `gemini.md` | Community standards |

### Theme

Set in `~/.local/share/crucible/crucible.json`:

```json
{
  "options": {
    "tui": {
      "theme": "amber-forge"
    }
  }
}
```

Available: `steel-blue` (default), `amber-forge`, `phosphor-green`, `reactor-red`, `titanium`, `clean-room`.

### Other options

| Option | Default | Description |
|--------|---------|-------------|
| `options.debug` | `false` | Enable debug logging |
| `options.tui.theme` | `steel-blue` | Color theme |
| `options.tui.spinner` | `meter` | Spinner style (`industrial`, `pulse`, `dots`, `ellipsis`, `points`, `meter`, `hamburger`, `trigram`) |
| `options.tui.compact_mode` | `false` | Compact TUI layout |
| `options.tui.diff_mode` | -- | Diff display (`unified`, `split`) |
| `options.tui.transparent` | `false` | Transparent background |
| `options.worktree` | `false` | Git worktree isolation per session |
| `options.attribution.trailer_style` | `assisted-by` | Commit trailer (`assisted-by`, `co-authored-by`, `none`) |
| `options.disable_auto_summarize` | `false` | Disable conversation summarization |
| `options.disable_provider_auto_update` | `false` | Disable provider catalog auto-update |
| `options.data_directory` | `.crucible` | Relative path for project data |
| `options.context_paths` | -- | Additional context file paths |
| `options.skills_paths` | -- | Paths to skill directories |
| `options.progress` | `true` | Show progress updates during long operations |
| `permissions.allowed_tools` | -- | Tools that skip permission prompts |
| `tools.grep.timeout` | `5s` | Grep tool timeout |
| `tools.ls.max_depth` | `0` | ls max directory depth |
| `tools.ls.max_items` | `1000` | ls max items returned |

## FEATURES

- **Supervisor delegation** -- Gemini reasons and routes; stations execute. The supervisor never writes code.
- **Station cards** -- Live activity trees, semantic state chips (Thinking, Reading, Editing, Testing, Running, Done, Failed, Canceled), elapsed time, verdict summaries.
- **Gate control** -- Per-station gates with operator approval. Runtime breakpoints via `Ctrl+H`.
- **ask_user tool** -- Structured operator questions from the supervisor when clarification is needed.
- **Context exhaustion recovery** -- Transparent station replacement when a station exhausts its context window (max 3 replacements with structured handoff).
- **Tool result steering** -- Post-station routing hints guide the supervisor's next action.
- **Google Search** -- Grounded answers with source attribution.
- **Theming** -- 6 built-in palettes: `steel-blue` (default), `amber-forge`, `phosphor-green`, `reactor-red`, `titanium`, `clean-room`. 4-tier derivation system (Core, Surfaces, Status, Semantic).
- **Session persistence** -- Dual SQLite databases for Crucible metadata and ADK conversation state.
- **MCP support** -- `stdio`, `http`, and `sse` transports for extending supervisor capabilities.

## ARCHITECTURE

```
crucible/
  cmd/                        CLI entry point (Cobra)
  internal/
    agent/                    ADK agent layer
      agent.go                Core agentic loop (ADK runner + event processing)
      agentrun.go             Station tool factory + process management
      coordinator.go          Model construction, provider resolution
      steering_plugin.go      Post-station routing hints
      gate.go                 Per-station gate enforcement
      askuser_tool.go         Structured operator questions
      loop_detection.go       Tool call loop detection
    ui/
      model/                  Bubble Tea v2 main model
      chat/                   Chat viewport + station card rendering
      styles/                 Theming system (6 palettes, 4-tier derivation)
      dialog/                 Modals and overlays
    session/                  Session metadata (title, cost, tokens)
    config/                   Configuration (models, stations, workflow)
    db/                       SQLite persistence (goose migrations)
    pubsub/                   Generic pub/sub broker
```

**Key dependencies:**

- [Google ADK](https://google.golang.org/adk) -- Agent framework (LLM interactions, tool execution, session management)
- [agentrun](https://github.com/dmora/agentrun) -- Spawns and manages external agent CLI processes
- [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + [Ultraviolet](https://github.com/charmbracelet/ultraviolet) -- TUI framework
- [Lipgloss v2](https://github.com/charmbracelet/lipgloss) -- Terminal styling

**Persistence:**

- `crucible.db` (goose/sqlc) -- Crucible session metadata, file history, stats
- `crucible-adk.db` (GORM) -- ADK conversation data (sessions, events, state)

## VISUAL DESIGN

Inspired by control panels in Alien, Blade Runner, Pacific Rim, Oblivion.

- Monochrome or limited palette on dark backgrounds
- Utilitarian monospace typography, ALL-CAPS labels
- Grid-based layouts with box-drawing borders
- Status readouts and fuel gauges for context windows
- Industrial nomenclature: STATION, SECTOR, PIPELINE

## TESTS

```bash
go test ./...
go vet ./...
```

## LOGGING

Logs are stored in `.crucible/logs/crucible.log` relative to the project root. Run with `-d` for debug-level logging.

```bash
crucible logs -f     # tail logs in real time
```

## ORIGIN

Forked from [charmbracelet/crush](https://github.com/charmbracelet/crush). The TUI infrastructure (Bubble Tea, Ultraviolet, dialog system, SQLite persistence) is retained. The agent layer has been completely replaced with Google ADK + agentrun.

## LICENSE

[MIT](LICENSE.md)
