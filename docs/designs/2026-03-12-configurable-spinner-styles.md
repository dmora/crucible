# ADR: Configurable Spinner Styles (#83)

**Status:** Proposed
**Date:** 2026-03-12
**Issue:** [#83](https://github.com/dmora/crucible/issues/83)

## Context

Crucible uses a custom `anim.Anim` spinner (multi-char gradient scramble with birth stagger and rebirth pulse). Users want to choose from simpler spinner styles. The `bubbles/v2/spinner` package (already a dependency) provides 5 additional presets. Both backends must work behind the existing `Animatable` interface without changing message routing.

## Decision

### 1. SpinnerBackend Interface

**Y-statement:** We chose to introduce a `SpinnerBackend` interface in the `anim` package, because it cleanly abstracts both animation backends behind a single type, accepting the cost of changing the field type in 3 structs (`baseToolMessageItem`, `AssistantMessageItem`, `ToolRenderOpts`), over modifying the existing `Animatable` interface (which is a UI-level concern about tea.Cmd lifecycle, not rendering).

```go
// anim/backend.go

// SpinnerBackend abstracts the rendering and lifecycle of a spinner animation.
// Both the industrial (anim.Anim) and classic (bubbles frame-cycling) backends
// implement this interface.
type SpinnerBackend interface {
    Start() tea.Cmd
    Animate(msg StepMsg) tea.Cmd
    Render() string
    SetLabel(label string)
    Width() int
}
```

**Why not extend `Animatable`?** `Animatable` (in `chat/messages.go`) is the message-item-level interface for the Bubble Tea update loop. It has `StartAnimation()` (which guards on `isSpinning()`) and `Animate()`. Adding `Render()`, `SetLabel()`, `Width()` would conflate UI lifecycle with rendering. `SpinnerBackend` is the inner engine; `Animatable` is the outer shell.

**Existing `*anim.Anim` already satisfies `SpinnerBackend`** — it has `Start()`, `Animate(StepMsg)`, `Render()`, `SetLabel(string)`, `Width() int`. Zero changes to `anim.Anim`.

### 2. Wrapping `bubbles/v2/spinner` — Classic Backend

#### Options Evaluated

| Option | Approach | Verdict |
|--------|----------|---------|
| **A: Use `spinner.Model` internally** | Wrapper holds `spinner.Model`, forges `spinner.TickMsg` on each `Animate()` call to drive `Model.Update()` | Rejected: `TickMsg.tag` is unexported, can't forge valid messages. `Model.Update()` returns a new model (value type), complicating state. Fragile coupling to internal spinner state. |
| **B: Dual message routing** | Classic spinner produces `spinner.TickMsg`; route both `StepMsg` and `TickMsg` through `Chat.Animate()` | Rejected: `spinner.TickMsg` is already handled by `todoSpinner` and command palette. Shared `TickMsg` type means every classic spinner would receive every tick, requiring additional filtering. High blast radius. |
| **C: Reimplement frame cycling** | Read `Spinner.Frames` and `Spinner.FPS` from bubbles presets. Drive frame advancement from `anim.StepMsg` ticks. No `spinner.Model` or `spinner.TickMsg` involved. | **Recommended.** Self-contained, zero interference with existing `spinner.TickMsg` handlers. Simple to test. |

**Y-statement:** We chose to reimplement frame cycling driven by `anim.StepMsg` (Option C), because it avoids coupling to unexported `spinner.TickMsg` internals and prevents interference with existing `spinner.TickMsg` routing (todoSpinner, command palette), accepting the cost of ~40 lines of frame-cycling code.

#### ClassicSpinner Implementation

```go
// anim/classic.go

type ClassicSpinner struct {
    id     string
    frames []string     // from bubbles preset
    fps    time.Duration // from bubbles preset, used for tick interval
    style  lipgloss.Style
    frame  atomic.Int64
    label  string        // stored but not rendered (no-op for classic)
}

func newClassicSpinner(id string, preset spinner.Spinner, fg color.Color) *ClassicSpinner {
    return &ClassicSpinner{
        id:     id,
        frames: preset.Frames,
        fps:    preset.FPS,
        style:  lipgloss.NewStyle().Foreground(fg),
    }
}

func (c *ClassicSpinner) Start() tea.Cmd {
    return c.step()
}

func (c *ClassicSpinner) Animate(msg StepMsg) tea.Cmd {
    if msg.ID != c.id {
        return nil
    }
    f := c.frame.Add(1)
    if int(f) >= len(c.frames) {
        c.frame.Store(0)
    }
    return c.step()
}

func (c *ClassicSpinner) Render() string {
    f := int(c.frame.Load()) % len(c.frames)
    return c.style.Render(c.frames[f])
}

func (c *ClassicSpinner) SetLabel(string) {} // no-op for classic spinners
func (c *ClassicSpinner) Width() int       { return lipgloss.Width(c.frames[0]) }

func (c *ClassicSpinner) step() tea.Cmd {
    id := c.id
    return tea.Tick(c.fps, func(t time.Time) tea.Msg {
        return StepMsg{ID: id}
    })
}
```

**Key design points:**
- Uses preset's native `FPS` for tick interval (not the industrial 20fps). Pulse ticks at 125ms, MiniDot at 83ms, etc.
- `SetLabel()` is a no-op. Classic spinners don't support labels (the "COMPACTING" label on assistant items). If the item is compacting, the industrial spinner is used regardless of preset (see factory logic below).
- ID-based routing works identically to `anim.Anim` — `StepMsg.ID` filters ensure no cross-talk.
- `frame` is `atomic.Int64` for thread safety, matching `anim.Anim`'s pattern.
- `style` applies theme `Primary` color for consistency across themes.

### 3. Factory Pattern

```go
// anim/factory.go

// Preset names as constants.
const (
    PresetIndustrial = "industrial"
    PresetPulse      = "pulse"
    PresetDots       = "dots"
    PresetEllipsis   = "ellipsis"
    PresetPoints     = "points"
    PresetMeter      = "meter"
)

// Presets returns the ordered list of all preset names.
func Presets() []string {
    return []string{
        PresetIndustrial, PresetPulse, PresetDots,
        PresetEllipsis, PresetPoints, PresetMeter,
    }
}

// presetMap maps preset names to bubbles spinner definitions.
var presetMap = map[string]spinner.Spinner{
    PresetPulse:    spinner.Pulse,
    PresetDots:     spinner.MiniDot,
    PresetEllipsis: spinner.Ellipsis,
    PresetPoints:   spinner.Points,
    PresetMeter:    spinner.Meter,
}

// NewSpinner returns a SpinnerBackend for the given preset.
// Industrial returns *Anim, all others return *ClassicSpinner.
func NewSpinner(preset string, opts Settings) SpinnerBackend {
    if preset == "" || preset == PresetIndustrial {
        return New(opts)
    }
    if sp, ok := presetMap[preset]; ok {
        return newClassicSpinner(opts.ID, sp, opts.GradColorA)
    }
    // Unknown preset: fall back to industrial.
    return New(opts)
}

// ValidPreset returns true if the preset name is recognized.
func ValidPreset(name string) bool {
    for _, p := range Presets() {
        if p == name {
            return true
        }
    }
    return false
}
```

**Why `GradColorA` for classic color?** All call sites set `GradColorA` to `sty.Primary`. Classic spinners need one foreground color. Reusing the existing settings field avoids a new parameter.

**Unknown preset fallback:** Returns industrial rather than erroring. Defensive against config file edits with typos. The setter (`SetSpinner`) validates; this is belt-and-suspenders.

### 4. Call Site Changes

All 4 call sites change from `anim.New(settings)` to `anim.NewSpinner(preset, settings)`:

| File | Current | New |
|------|---------|-----|
| `chat/assistant.go:53` | `anim.New(anim.Settings{...})` | `anim.NewSpinner(sty.SpinnerPreset, anim.Settings{...})` |
| `chat/tools.go:165` | `anim.New(anim.Settings{...})` | `anim.NewSpinner(sty.SpinnerPreset, anim.Settings{...})` |
| `format/spinner.go:47` | `anim.New(animSettings)` | `anim.NewSpinner(preset, animSettings)` |
| `app/app.go:257` | `anim.Settings{...}` | `anim.NewSpinner(preset, anim.Settings{...})` |

**Type changes:**

| Struct | Field | Before | After |
|--------|-------|--------|-------|
| `baseToolMessageItem` | `anim` | `*anim.Anim` | `anim.SpinnerBackend` |
| `AssistantMessageItem` | `anim` | `*anim.Anim` | `anim.SpinnerBackend` |
| `ToolRenderOpts` | `Anim` | `*anim.Anim` | `anim.SpinnerBackend` |

**Preset propagation:** The preset string needs to reach the call sites. Two options:

| Option | How | Trade-off |
|--------|-----|-----------|
| **A: Via `*styles.Styles`** | Add `SpinnerPreset string` field to `Styles`. Set during `NewStyles()` from config. All call sites already receive `sty`. | Clean — follows theme precedent. `Styles` already carries theme state. Preset travels with styles through the existing dependency graph. |
| **B: Via config lookup** | Call sites read `config.SpinnerPreset()` directly. | Requires config dependency in `chat` package, which currently only depends on `styles`. Breaks layering. |

**Recommended: Option A.** `styles.Styles` already carries theme identity (`Primary`, `Secondary`, etc.). Adding `SpinnerPreset string` is consistent. Updated in `applySpinner()` alongside theme-level changes.

### 5. Config Integration

Follows the `options.tui.theme` pattern exactly:

```go
// config/config.go additions

// In TUIOptions:
type TUIOptions struct {
    CompactMode bool   `json:"compact_mode,omitempty"`
    DiffMode    string `json:"diff_mode,omitempty"`
    Theme       string `json:"theme,omitempty" ...`
    Spinner     string `json:"spinner,omitempty" jsonschema:"...,enum=industrial,...,default=industrial"`
    // ...
}

// validSpinnerPresets mirrors validThemeIDs.
var validSpinnerPresets = map[string]bool{
    "industrial": true, "pulse": true, "dots": true,
    "ellipsis": true, "points": true, "meter": true,
}

func (c *Config) SpinnerPreset() string {
    if c.Options != nil && c.Options.TUI != nil && c.Options.TUI.Spinner != "" {
        return c.Options.TUI.Spinner
    }
    return "industrial"
}

func (c *Config) SetSpinner(preset string) error {
    if !validSpinnerPresets[preset] {
        return fmt.Errorf("unknown spinner preset %q", preset)
    }
    if c.Options == nil { c.Options = &Options{} }
    if c.Options.TUI == nil { c.Options.TUI = &TUIOptions{} }
    c.Options.TUI.Spinner = preset
    return c.SetConfigField("options.tui.spinner", preset)
}
```

**Persistence path:** `SetSpinner()` → `SetConfigField("options.tui.spinner", preset)` → writes to `~/.local/share/crucible/crucible.json` via `sjson.Set`. Same as theme.

### 6. GUI Picker — Spinner Dialog

Clone the theme dialog pattern. New file `internal/ui/dialog/spinner_picker.go`.

```
SpinnerDialogID = "spinner"

Command palette entry:
  NewCommandItem(styles, "switch_spinner", "Switch Spinner", "ctrl+shift+s", ActionOpenDialog{
      DialogID: SpinnerDialogID,
  })

Action type:
  ActionSelectSpinner struct { Preset string }
```

**Dialog structure:**
- `FilterableList` of 6 presets
- Each item shows: preset name + description + "current" badge
- Descriptions: `"Multi-char gradient scramble"`, `"Fading block pulse"`, `"Braille rotation"`, `"Growing dots"`, `"Traveling dot"`, `"Fill bar"`
- No color swatch needed (unlike theme) — the description is sufficient
- Keyboard: up/down navigate, Enter selects, Esc closes, type to filter

**`applySpinner()` in `ui.go`:**

```go
func (m *UI) applySpinner(preset string) tea.Cmd {
    // 1. Persist to config
    m.com.Config().SetSpinner(preset)

    // 2. Update styles (carries preset to call sites)
    m.com.Styles.SpinnerPreset = preset

    // 3. Rebuild chat messages from session (same as applyTheme)
    //    This recreates all message items, which calls NewSpinner() with the new preset.
    return m.rebuildSessionMessages()
}
```

**Why rebuild messages?** Active spinners hold a `SpinnerBackend` instance. Changing the backend requires recreating the message items. The theme change already does this via `applyTheme()` → `setSessionMessages()`. We reuse the same mechanism. This is ~50ms for a typical session — imperceptible.

**Alternative considered: hot-swap backend on active spinners.** Would require a `SetBackend()` method on message items, careful tick chain management (old backend's `StepMsg` still in flight), and touching every `Animatable` implementation. High complexity for no perceptible UX benefit. Rejected.

### 7. AssistantMessageItem Compacting Edge Case

`AssistantMessageItem` calls `a.anim.SetLabel("COMPACTING")` during compaction (line 279). Classic spinners no-op `SetLabel()`. Two options:

| Option | Behavior during compaction |
|--------|---------------------------|
| **A: Always use industrial for compacting** | Factory check: if label is needed, force industrial | Over-complex. Label is set post-construction, not at factory time. |
| **B: No-op is fine** | Classic spinner renders without label. Compacting state is already communicated via the message text "Compacting..." | **Recommended.** The label is supplementary, not essential. |

### 8. `animCacheMap` Impact

`animCacheMap` is keyed by `settingsHash(opts Settings)`. Classic spinners don't use the cache (they have no expensive pre-rendering). Industrial spinners continue using it unchanged.

No changes needed to the cache. The factory branches before cache lookup.

### 9. Non-Interactive (CLI) Spinner

`format/Spinner` and `app/app.go` create spinners for the non-TUI `run` command. These should also respect the config:

```go
// app/app.go
preset := cfg.SpinnerPreset()
backend := anim.NewSpinner(preset, anim.Settings{...})
```

`format/Spinner` wraps a `tea.Program` that handles `anim.StepMsg`. Since `ClassicSpinner` also produces `anim.StepMsg`, it works without changes to the `format.model` Update loop.

## File Inventory

| File | Change |
|------|--------|
| `internal/ui/anim/backend.go` | **New.** `SpinnerBackend` interface definition. |
| `internal/ui/anim/classic.go` | **New.** `ClassicSpinner` type + `newClassicSpinner()`. |
| `internal/ui/anim/factory.go` | **New.** `NewSpinner()`, `Presets()`, `ValidPreset()`, preset constants, `presetMap`. |
| `internal/config/config.go` | **Edit.** `Spinner` field on `TUIOptions`, `SpinnerPreset()`, `SetSpinner()`, `validSpinnerPresets`. |
| `internal/ui/styles/styles.go` | **Edit.** Add `SpinnerPreset string` field to `Styles`. Set from config in `NewStyles()`. |
| `internal/ui/chat/tools.go` | **Edit.** `baseToolMessageItem.anim` type → `anim.SpinnerBackend`. `ToolRenderOpts.Anim` type → `anim.SpinnerBackend`. Call site → `anim.NewSpinner()`. |
| `internal/ui/chat/assistant.go` | **Edit.** `AssistantMessageItem.anim` type → `anim.SpinnerBackend`. Call site → `anim.NewSpinner()`. |
| `internal/ui/dialog/spinner_picker.go` | **New.** `SpinnerPicker` dialog (clone of `theme.go`). |
| `internal/ui/dialog/actions.go` | **Edit.** Add `ActionSelectSpinner` type. |
| `internal/ui/dialog/commands.go` | **Edit.** Add "Switch Spinner" command in `defaultCommands()`. |
| `internal/ui/model/ui.go` | **Edit.** Handle `ActionSelectSpinner`, `applySpinner()` method. |
| `internal/app/app.go` | **Edit.** Use `anim.NewSpinner()` with config preset. |
| `internal/format/spinner.go` | **Edit.** Accept preset parameter, use `anim.NewSpinner()`. |

**New files: 4.** Edits to existing: 8. No deletions.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Classic spinner width varies per frame (e.g., Ellipsis: `""` → `"..."`) | `Width()` returns width of first frame. Layout code already handles variable-width spinners (industrial scramble + label can vary). No change needed. |
| Tick interval mismatch (industrial 50ms vs Pulse 125ms) | Each backend uses its own interval. `StepMsg` routing is ID-based, so different intervals coexist safely. |
| `spinner.TickMsg` interference | Option C avoids `spinner.TickMsg` entirely. Classic spinners only produce `anim.StepMsg`. Zero interference. |
| Config migration (existing users have no `spinner` field) | `SpinnerPreset()` defaults to `"industrial"` when field is empty. Zero-migration, backward compatible. |

## Sequence: Build Order

1. **`anim/backend.go`** — Interface definition (enables compilation of all downstream)
2. **`anim/classic.go`** — ClassicSpinner implementation
3. **`anim/factory.go`** — Factory + presets + validation
4. **`config/config.go`** — Config field, getter, setter
5. **`styles/styles.go`** — `SpinnerPreset` field propagation
6. **`chat/tools.go` + `chat/assistant.go`** — Type changes + factory call sites
7. **`app/app.go` + `format/spinner.go`** — CLI spinner call sites
8. **`dialog/actions.go`** — `ActionSelectSpinner` type
9. **`dialog/spinner_picker.go`** — Picker dialog
10. **`dialog/commands.go`** — Command palette entry
11. **`model/ui.go`** — Action handler + `applySpinner()`
