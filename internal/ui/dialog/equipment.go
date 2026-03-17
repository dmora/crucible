package dialog

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/agent/tools/mcp"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// EquipmentID is the identifier for the equipment dialog.
const EquipmentID = "equipment"

type equipmentMode uint8

const (
	equipmentModeList equipmentMode = iota
	equipmentModeDetail
)

// EquipmentKind categorises equipment entries.
type EquipmentKind uint8

const (
	EquipmentStation EquipmentKind = iota
	EquipmentMCP
	EquipmentProvider
)

func (k EquipmentKind) String() string {
	return [...]string{"STATION", "MCP", "PROVIDER"}[k]
}

// EquipmentEntry holds the display data for a single equipment row.
type EquipmentEntry struct {
	Kind        EquipmentKind
	Name        string
	Summary     string // one-line description for the list
	DetailLines string // multi-line detail for the detail view
}

// Equipment is a two-mode dialog for inspecting factory configuration.
type Equipment struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model
	mode  equipmentMode

	entries []EquipmentEntry

	// Detail view state.
	viewTitle      string
	viewRawContent string // raw detail text for clipboard copy
	viewScroll     int
	viewHeight     int
	viewLines      []string

	// Mouse selection state (detail mode only).
	mouseDown   bool
	mouseStartX int             // content-relative column where drag started
	mouseStartY int             // content-relative line where drag started
	mouseDragX  int             // current drag column
	mouseDragY  int             // current drag line
	contentArea image.Rectangle // screen-space rect of scrollable content, cached in Draw

	keyMap struct {
		Select     key.Binding
		Next       key.Binding
		Previous   key.Binding
		UpDown     key.Binding
		Back       key.Binding
		Close      key.Binding
		ScrollDown key.Binding
		ScrollUp   key.Binding
		PageDown   key.Binding
		PageUp     key.Binding
		Copy       key.Binding
	}
}

var _ Dialog = (*Equipment)(nil)

// NewEquipment creates a new Equipment dialog populated from config and MCP state.
func NewEquipment(com *common.Common, mcpStates map[string]mcp.ClientInfo) *Equipment {
	e := new(Equipment)
	e.com = com
	e.mode = equipmentModeList

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	e.help = h

	e.input = textinput.New()
	e.input.SetVirtualCursor(false)
	e.input.Placeholder = "Filter equipment"
	e.input.SetStyles(com.Styles.TextInput)
	e.input.Focus()

	e.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view"))
	e.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next"))
	e.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "previous"))
	e.keyMap.UpDown = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "navigate"))
	e.keyMap.Back = key.NewBinding(key.WithKeys("backspace"), key.WithHelp("bksp", "back"))
	e.keyMap.Close = CloseKey
	e.keyMap.ScrollDown = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down"))
	e.keyMap.ScrollUp = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up"))
	e.keyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "f", " "), key.WithHelp("f/pgdn", "page down"))
	e.keyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("b/pgup", "page up"))
	e.keyMap.Copy = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy"))

	e.entries = buildEquipmentEntries(com.Config(), mcpStates)
	e.list = list.NewFilterableList(equipmentItems(com.Styles, e.entries)...)
	e.list.Focus()
	return e
}

// ID implements Dialog.
func (e *Equipment) ID() string { return EquipmentID }

// HandleMsg implements Dialog.
func (e *Equipment) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch e.mode {
		case equipmentModeList:
			return e.handleListKey(msg)
		case equipmentModeDetail:
			return e.handleDetailKey(msg)
		}
	case tea.MouseClickMsg:
		if e.mode == equipmentModeDetail {
			return e.handleDetailMouseDown(msg)
		}
	case tea.MouseMotionMsg:
		if e.mode == equipmentModeDetail {
			e.handleDetailMouseDrag(msg)
		}
	case tea.MouseReleaseMsg:
		if e.mode == equipmentModeDetail {
			return e.handleDetailMouseUp(msg)
		}
	}
	return nil
}

func (e *Equipment) handleListKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, e.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, e.keyMap.Previous):
		e.list.Focus()
		if e.list.IsSelectedFirst() {
			e.list.SelectLast()
		} else {
			e.list.SelectPrev()
		}
		e.list.ScrollToSelected()
	case key.Matches(msg, e.keyMap.Next):
		e.list.Focus()
		if e.list.IsSelectedLast() {
			e.list.SelectFirst()
		} else {
			e.list.SelectNext()
		}
		e.list.ScrollToSelected()
	case key.Matches(msg, e.keyMap.Select):
		item := e.list.SelectedItem()
		if item == nil {
			return nil
		}
		ei, ok := item.(*EquipmentItem)
		if !ok {
			return nil
		}
		e.enterDetailMode(ei.entry)
	default:
		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		e.list.SetFilter(e.input.Value())
		e.list.ScrollToTop()
		e.list.SetSelected(0)
		return ActionCmd{cmd}
	}
	return nil
}

func (e *Equipment) handleDetailKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, e.keyMap.Copy):
		cmd := common.CopyToClipboard(e.viewRawContent, "Equipment detail copied to clipboard")
		return ActionCmd{cmd}
	case key.Matches(msg, e.keyMap.Back), key.Matches(msg, e.keyMap.Close):
		e.clearSelection()
		e.mode = equipmentModeList
	case key.Matches(msg, e.keyMap.ScrollDown):
		e.clearSelection()
		e.scrollBy(1)
	case key.Matches(msg, e.keyMap.ScrollUp):
		e.clearSelection()
		e.scrollBy(-1)
	case key.Matches(msg, e.keyMap.PageDown):
		e.clearSelection()
		e.scrollBy(e.viewHeight)
	case key.Matches(msg, e.keyMap.PageUp):
		e.clearSelection()
		e.scrollBy(-e.viewHeight)
	}
	return nil
}

func (e *Equipment) handleDetailMouseDown(msg tea.MouseClickMsg) Action {
	if !image.Pt(msg.X, msg.Y).In(e.contentArea) {
		return nil
	}
	e.mouseDown = true
	e.mouseStartX = msg.X - e.contentArea.Min.X
	e.mouseStartY = msg.Y - e.contentArea.Min.Y
	e.mouseDragX = e.mouseStartX
	e.mouseDragY = e.mouseStartY
	return nil
}

func (e *Equipment) handleDetailMouseDrag(msg tea.MouseMotionMsg) {
	if !e.mouseDown {
		return
	}
	e.mouseDragX = max(0, min(msg.X-e.contentArea.Min.X, e.contentArea.Dx()-1))
	e.mouseDragY = max(0, min(msg.Y-e.contentArea.Min.Y, e.contentArea.Dy()-1))
}

func (e *Equipment) handleDetailMouseUp(_ tea.MouseReleaseMsg) Action {
	if !e.mouseDown {
		return nil
	}
	e.mouseDown = false

	sLine, sCol, eLine, eCol, ok := e.normalizedSelection()
	if !ok {
		return nil
	}

	end := min(e.viewScroll+e.viewHeight, len(e.viewLines))
	start := min(e.viewScroll, end)
	visible := strings.Join(e.viewLines[start:end], "\n")
	text := list.HighlightContent(visible,
		image.Rect(0, 0, e.contentArea.Dx(), e.contentArea.Dy()),
		sLine, sCol, eLine, eCol)
	text = strings.TrimRight(text, "\n")

	if text == "" {
		e.clearSelection()
		return nil
	}

	cmd := common.CopyToClipboardWithCallback(text, "Copied to clipboard", func() tea.Msg {
		e.clearSelection()
		return nil
	})
	return ActionCmd{cmd}
}

func (e *Equipment) normalizedSelection() (int, int, int, int, bool) {
	sY, sX := e.mouseStartY, e.mouseStartX
	eY, eX := e.mouseDragY, e.mouseDragX
	if sY == eY && sX == eX {
		return 0, 0, 0, 0, false
	}
	if sY > eY || (sY == eY && sX > eX) {
		sY, sX, eY, eX = eY, eX, sY, sX
	}
	return sY, sX, eY, eX, true
}

func (e *Equipment) clearSelection() {
	e.mouseDown = false
	e.mouseStartX = 0
	e.mouseStartY = 0
	e.mouseDragX = 0
	e.mouseDragY = 0
}

func (e *Equipment) enterDetailMode(entry EquipmentEntry) {
	e.mode = equipmentModeDetail
	e.viewScroll = 0
	e.viewTitle = entry.Kind.String() + ": " + entry.Name
	e.viewRawContent = entry.DetailLines
	e.viewLines = strings.Split(entry.DetailLines, "\n")
}

func (e *Equipment) scrollBy(delta int) {
	e.viewScroll += delta
	maxScroll := max(0, len(e.viewLines)-e.viewHeight)
	e.viewScroll = max(0, min(e.viewScroll, maxScroll))
}

func (e *Equipment) drawDetailMode(scr uv.Screen, area uv.Rectangle, rc *RenderContext, t *styles.Styles, height, innerWidth int) {
	rc.Title = e.viewTitle
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	e.viewHeight = max(1, height-heightOffset)
	e.help.SetWidth(innerWidth)

	// Render visible slice of content.
	end := min(e.viewScroll+e.viewHeight, len(e.viewLines))
	start := min(e.viewScroll, end)
	visible := strings.Join(e.viewLines[start:end], "\n")

	// Apply mouse selection highlight if active.
	if sLine, sCol, eLine, eCol, ok := e.normalizedSelection(); ok {
		visible = list.Highlight(visible,
			image.Rect(0, 0, innerWidth, e.viewHeight),
			sLine, sCol, eLine, eCol,
			list.DefaultHighlighter)
	}

	contentView := lipgloss.NewStyle().
		Width(innerWidth).
		Height(e.viewHeight).
		Render(visible)
	rc.AddPart(contentView)

	rc.Help = e.help.View(e)
	view := rc.Render()
	DrawCenter(scr, area, view)
	e.cacheContentArea(t, area, view, innerWidth)
}

func (e *Equipment) cacheContentArea(t *styles.Styles, area uv.Rectangle, view string, innerWidth int) {
	dialogW, dialogH := lipgloss.Size(view)
	center := common.CenterRect(area, dialogW, dialogH)
	contentTop := center.Min.Y +
		t.Dialog.View.GetBorderTopSize() +
		t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight
	contentLeft := center.Min.X +
		t.Dialog.View.GetBorderLeftSize()
	e.contentArea = image.Rect(contentLeft, contentTop, contentLeft+innerWidth, contentTop+e.viewHeight)
}

// Draw implements Dialog.
func (e *Equipment) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := e.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)

	switch e.mode {
	case equipmentModeList:
		rc.Title = "Equipment"
		heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
			t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
			t.Dialog.HelpView.GetVerticalFrameSize() +
			t.Dialog.View.GetVerticalFrameSize()

		e.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
		e.list.SetSize(innerWidth, height-heightOffset)
		e.help.SetWidth(innerWidth)

		inputView := t.Dialog.InputPrompt.Render(e.input.View())
		rc.AddPart(inputView)

		if len(e.entries) == 0 {
			emptyView := lipgloss.NewStyle().
				Foreground(t.Subtle.GetForeground()).
				Width(innerWidth).
				Render("No equipment configured")
			rc.AddPart(emptyView)
		} else {
			listView := t.Dialog.List.Height(e.list.Height()).Render(e.list.Render())
			rc.AddPart(listView)
		}

		rc.Help = e.help.View(e)
		view := rc.Render()
		cur := InputCursor(t, e.input.Cursor())
		DrawCenterCursor(scr, area, view, cur)
		return cur

	case equipmentModeDetail:
		e.drawDetailMode(scr, area, rc, t, height, innerWidth)
		return nil
	}

	return nil
}

// ShortHelp implements help.KeyMap.
func (e *Equipment) ShortHelp() []key.Binding {
	switch e.mode {
	case equipmentModeDetail:
		return []key.Binding{
			e.keyMap.ScrollUp, e.keyMap.ScrollDown,
			e.keyMap.PageUp, e.keyMap.PageDown,
			e.keyMap.Copy,
			e.keyMap.Back, e.keyMap.Close,
		}
	default:
		return []key.Binding{e.keyMap.UpDown, e.keyMap.Select, e.keyMap.Close}
	}
}

// FullHelp implements help.KeyMap.
func (e *Equipment) FullHelp() [][]key.Binding {
	bindings := e.ShortHelp()
	var rows [][]key.Binding
	for i := 0; i < len(bindings); i += 4 {
		end := min(i+4, len(bindings))
		rows = append(rows, bindings[i:end])
	}
	return rows
}

// --- EquipmentItem (list item) ---

// EquipmentItem wraps an EquipmentEntry to implement the ListItem interface.
type EquipmentItem struct {
	entry   EquipmentEntry
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var _ ListItem = &EquipmentItem{}

func (i *EquipmentItem) ID() string { return i.entry.Kind.String() + ":" + i.entry.Name }
func (i *EquipmentItem) Filter() string {
	return i.entry.Kind.String() + " " + i.entry.Name + " " + i.entry.Summary
}
func (i *EquipmentItem) SetMatch(m fuzzy.Match) { i.cache = nil; i.m = m }
func (i *EquipmentItem) SetFocused(focused bool) {
	if i.focused != focused {
		i.cache = nil
	}
	i.focused = focused
}

func (i *EquipmentItem) Render(width int) string {
	if cached, ok := i.cache[width]; ok {
		return cached
	}

	rowStyle := i.t.Dialog.NormalItem
	if i.focused {
		rowStyle = i.t.Dialog.SelectedItem
	}

	chip := i.t.TagInfo.Bold(true).Render(i.entry.Kind.String())
	name := i.t.Tool.NameNormal.Render(i.entry.Name)
	titleLine := chip + " " + name

	descStyle := i.t.Subtle
	if i.focused {
		descStyle = i.t.Base
	}
	descLine := descStyle.Render(i.entry.Summary)

	content := titleLine + "\n" + descLine
	result := rowStyle.Width(width).Render(content)

	if i.cache == nil {
		i.cache = make(map[int]string)
	}
	i.cache[width] = result
	return result
}

func equipmentItems(t *styles.Styles, entries []EquipmentEntry) []list.FilterableItem {
	items := make([]list.FilterableItem, len(entries))
	for idx, e := range entries {
		items[idx] = &EquipmentItem{entry: e, t: t}
	}
	return items
}

// --- Data builders ---

func buildEquipmentEntries(cfg *config.Config, mcpStates map[string]mcp.ClientInfo) []EquipmentEntry {
	stationEntries := buildStationEntries(cfg.Stations)
	mcpEntries := buildMCPEntries(cfg, mcpStates)
	providerEntries := buildProviderEntries(cfg)
	entries := make([]EquipmentEntry, 0, len(stationEntries)+len(mcpEntries)+len(providerEntries))
	entries = append(entries, stationEntries...)
	entries = append(entries, mcpEntries...)
	entries = append(entries, providerEntries...)
	return entries
}

func buildStationEntries(stations map[string]config.StationConfig) []EquipmentEntry {
	names := make([]string, 0, len(stations))
	for name := range stations {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]EquipmentEntry, 0, len(names))
	for _, name := range names {
		sc := stations[name]
		if sc.Disabled {
			continue
		}
		entries = append(entries, EquipmentEntry{
			Kind:        EquipmentStation,
			Name:        name,
			Summary:     stationSummary(sc),
			DetailLines: stationDetail(name, sc),
		})
	}
	return entries
}

func stationSummary(sc config.StationConfig) string {
	parts := []string{cmpOr(sc.Backend, "claude")}
	if mode := sc.Options["mode"]; mode != "" {
		parts = append(parts, mode)
	}
	if sc.Skill != "" {
		if idx := strings.LastIndex(sc.Skill, ":"); idx >= 0 && idx < len(sc.Skill)-1 {
			parts = append(parts, sc.Skill[idx+1:])
		} else {
			parts = append(parts, sc.Skill)
		}
	}
	if sc.Gate {
		parts = append(parts, "gate")
	}
	return strings.Join(parts, " · ")
}

func stationDetail(name string, sc config.StationConfig) string {
	lines := []string{
		fmt.Sprintf("Name:        %s", name),
		fmt.Sprintf("Backend:     %s", cmpOr(sc.Backend, "claude")),
	}
	if sc.Model != "" {
		lines = append(lines, fmt.Sprintf("Model:       %s", sc.Model))
	}
	if mode := sc.Options["mode"]; mode != "" {
		lines = append(lines, fmt.Sprintf("Mode:        %s", mode))
	}
	if sc.Skill != "" {
		lines = append(lines, fmt.Sprintf("Skill:       %s", sc.Skill))
	}
	if sc.Gate {
		lines = append(lines, "Gate:        yes")
	}
	if sc.Description != "" {
		lines = append(lines, "", "Description:", sc.Description)
	}
	if sc.Steering != "" {
		lines = append(lines, "", "Steering:", sc.Steering)
	}
	lines = append(lines, sortedMapSection("Environment", sc.Env)...)
	lines = append(lines, stationOptionsSection(sc.Options)...)
	return strings.Join(lines, "\n")
}

func sortedMapSection(header string, m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{"", header + ":"}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %s=%s", k, m[k]))
	}
	return lines
}

func stationOptionsSection(opts map[string]string) []string {
	if len(opts) == 0 {
		return nil
	}
	var lines []string
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "mode" {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s=%s", k, opts[k]))
	}
	if len(lines) == 0 {
		return nil
	}
	return append([]string{"", "Options:"}, lines...)
}

func buildMCPEntries(cfg *config.Config, mcpStates map[string]mcp.ClientInfo) []EquipmentEntry {
	sorted := cfg.MCP.Sorted()
	entries := make([]EquipmentEntry, 0, len(sorted))
	for _, mcpCfg := range sorted {
		state, ok := mcpStates[mcpCfg.Name]
		summary := "not connected"
		if ok {
			switch state.State {
			case mcp.StateConnected:
				summary = fmt.Sprintf("connected · %d tools", state.Counts.Tools)
			case mcp.StateStarting:
				summary = "starting"
			case mcp.StateError:
				summary = "error"
			default:
				summary = "offline"
			}
		}
		detail := fmt.Sprintf("Name:    %s\nState:   %s", mcpCfg.Name, summary)
		if ok && state.State == mcp.StateConnected {
			detail += fmt.Sprintf("\nTools:   %d", state.Counts.Tools)
			if state.Counts.Prompts > 0 {
				detail += fmt.Sprintf("\nPrompts: %d", state.Counts.Prompts)
			}
			if state.Counts.Resources > 0 {
				detail += fmt.Sprintf("\nResources: %d", state.Counts.Resources)
			}
		}
		entries = append(entries, EquipmentEntry{
			Kind:        EquipmentMCP,
			Name:        mcpCfg.Name,
			Summary:     summary,
			DetailLines: detail,
		})
	}
	return entries
}

func buildProviderEntries(cfg *config.Config) []EquipmentEntry {
	if cfg.Providers == nil {
		return nil
	}
	// Collect and sort provider names.
	type provKV struct {
		id  string
		cfg config.ProviderConfig
	}
	var providers []provKV
	for id, pc := range cfg.Providers.Seq2() {
		providers = append(providers, provKV{id, pc})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].id < providers[j].id })

	entries := make([]EquipmentEntry, 0, len(providers))
	for _, p := range providers {
		summary := cmpOr(p.cfg.Name, p.id)
		if p.cfg.Type != "" {
			summary += " · " + p.cfg.Type
		}
		detail := fmt.Sprintf("ID:      %s", p.id)
		if p.cfg.Name != "" {
			detail += fmt.Sprintf("\nName:    %s", p.cfg.Name)
		}
		if p.cfg.Type != "" {
			detail += fmt.Sprintf("\nType:    %s", p.cfg.Type)
		}
		if p.cfg.BaseURL != "" {
			detail += fmt.Sprintf("\nBaseURL: %s", p.cfg.BaseURL)
		}
		hasKey := p.cfg.APIKey != ""
		detail += fmt.Sprintf("\nAPI Key: %v", hasKey)
		entries = append(entries, EquipmentEntry{
			Kind:        EquipmentProvider,
			Name:        p.id,
			Summary:     summary,
			DetailLines: detail,
		})
	}
	return entries
}

// cmpOr returns a if non-empty, else b.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
