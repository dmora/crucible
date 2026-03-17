---
description: Bubble Tea v2, ultraviolet rendering, lipgloss styling, station card rendering patterns
paths: ["internal/ui/**"]
---

# UI Layer Patterns

## Framework Stack

- **Bubble Tea v2** — value-type model, `Update(tea.Msg) (tea.Model, tea.Cmd)`, goroutine+channel bridge for async
- **Ultraviolet** — screen-buffer rendering with zone-based layout (`uv.Screen`, `uv.Drawable`)
- **Lipgloss v2** — styling (borders, colors, padding). Styles built from theme palette.

## Theming System

5 built-in themes with 4-tier palette: Core identity (Primary/Secondary/Tertiary) → Surfaces → Status → Semantic. `DeriveDefaults()` fills Tier 2-4 from Tier 1.

**Themes:** `steel-blue` (default), `amber-forge`, `phosphor-green`, `reactor-red`, `titanium`.

**Key files:** `styles/theme.go` (Palette, registry, derivation), `themes_builtin.go` (5 palettes), `styles.go` (`NewStyles(id)` → `buildStyles(palette)`).

**Config:** `options.tui.theme` in `~/.local/share/crucible/crucible.json`.

## Station Card Rendering

Station cards (`StationToolMessageItem` in `chat/testbench.go`) are operator-optimized information radiators:

- **Header**: station name + state chip (Thinking/Reading/Editing/Testing/Running/Searching/Done/Failed/Canceled) + elapsed
- **Task**: prompt sent to station
- **Activity tree**: last 5 entries, operator-friendly labels, consecutive similar activities collapsed
- **Verdict**: outcome + file/command counters + first result line
- **Compact mode**: `Station · State · Elapsed · Deltas`

**Key files:** `chat/testbench.go` (rendering), `chat/station_summary.go` (`StationSummary`, `DeriveOperatorState()`), `common/time.go` (`FormatElapsed()`).

**Sidebar** (`model/process.go`) shows station name + state + model + fuel gauge + elapsed, using same `DeriveOperatorState()`.

## Dynamic Widget Matching

`RegisterStationNames()` at UI init populates registry from `config.Stations`. `IsStationTool()` checked in default branch of `NewToolMessageItem()` factory. Adding stations is config-only — no UI code changes needed.

## Visual Design

Monochrome/limited palette, ALL-CAPS labels, grid layouts, box-drawing borders. Token budgets as fuel gauges, workflow steps as pipeline stages. Industrial nomenclature: "STATION", "SECTOR", "PIPELINE".
