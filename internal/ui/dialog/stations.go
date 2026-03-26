package dialog

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/util"
)

// StationsID is the identifier for the Stations dialog.
const StationsID = "stations"

type stationsMode int

const (
	stationsModeList stationsMode = iota
	stationsModeEdit
)

const (
	stationsDialogMaxWidth  = 70
	stationsDialogMaxHeight = 20
)

// Stations is a two-mode dialog for viewing and editing station configuration.
type Stations struct {
	com  *common.Common
	mode stationsMode
	list *list.FilterableList
	form *Form
	help help.Model

	editName string
	editCfg  config.StationConfig

	keyMap struct {
		Select, Toggle, Reset, Delete, Close key.Binding
	}
}

var _ Dialog = (*Stations)(nil)

// NewStations creates a new Stations dialog in list mode.
func NewStations(com *common.Common) (*Stations, tea.Cmd) {
	s := &Stations{com: com, mode: stationsModeList}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	s.help = h

	s.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit"))
	s.keyMap.Toggle = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle"))
	s.keyMap.Reset = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reset"))
	s.keyMap.Delete = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	s.keyMap.Close = CloseKey

	s.buildList()
	return s, nil
}

func (s *Stations) buildList() {
	cfg := s.com.Config()
	names := sortedStationNames(cfg.Stations)

	items := make([]list.FilterableItem, 0, len(names))
	for _, name := range names {
		sc := cfg.Stations[name]
		_, isBuiltin := config.DefaultStations[name]
		items = append(items, newStationItem(s.com.Styles, name, sc, isBuiltin, cfg))
	}
	s.list = list.NewFilterableList(items...)
	s.list.Focus()
}

func sortedStationNames(stations map[string]config.StationConfig) []string {
	names := make([]string, 0, len(stations))
	for name := range stations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ID returns the dialog identifier.
func (s *Stations) ID() string { return StationsID }

// ShortHelp returns help bindings for the current mode.
func (s *Stations) ShortHelp() []key.Binding {
	if s.mode == stationsModeEdit {
		return FormHelpBindings()
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("↑↓"), key.WithHelp("↑/↓", "choose")),
		s.keyMap.Select, s.keyMap.Toggle, s.keyMap.Reset, s.keyMap.Close,
	}
}

// FullHelp returns full help bindings.
func (s *Stations) FullHelp() [][]key.Binding {
	return [][]key.Binding{s.ShortHelp()}
}

// HandleMsg processes input for the Stations dialog.
func (s *Stations) HandleMsg(msg tea.Msg) Action {
	if s.mode == stationsModeEdit {
		return s.handleEditMsg(msg)
	}
	return s.handleListMsg(msg)
}

func (s *Stations) handleListMsg(msg tea.Msg) Action {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, s.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, s.keyMap.Select):
			return s.enterEditMode()
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			s.list.SelectNext()
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			s.list.SelectPrev()
		case key.Matches(msg, s.keyMap.Toggle):
			return s.toggleDisabled()
		case key.Matches(msg, s.keyMap.Reset):
			return s.resetStation()
		case key.Matches(msg, s.keyMap.Delete):
			return s.deleteStation()
		}
	}
	return nil
}

func (s *Stations) handleEditMsg(msg tea.Msg) Action {
	result := s.form.HandleMsg(msg)
	switch result {
	case FormSave:
		return s.saveStation()
	case FormCancel:
		s.mode = stationsModeList
	}
	return nil
}

func (s *Stations) enterEditMode() Action {
	item, ok := s.list.SelectedItem().(*StationItem)
	if !ok {
		return nil
	}

	cfg := s.com.Config()
	scfg := cfg.Stations[item.name]
	s.editName = item.name
	s.editCfg = scfg

	mode := ""
	if scfg.Options != nil {
		mode = scfg.Options["mode"]
	}

	// Determine current scope for the cycle selector.
	currentScope := cfg.StationScope(item.name)
	scopeValue := "global"
	if currentScope == config.ConfigScopeProject {
		scopeValue = "project"
	}

	scopeOptions := []string{"global"}
	if cfg.ProjectConfigPath() != "" {
		scopeOptions = append(scopeOptions, "project")
	}

	fields := []FormField{
		{Label: "Name", Key: "name", Kind: FormFieldText, Value: item.name, ReadOnly: item.isBuiltin},
		{Label: "Scope", Key: "scope", Kind: FormFieldCycle, Value: scopeValue, Options: scopeOptions},
		{
			Label: "Backend", Key: "backend", Kind: FormFieldCycle, Value: scfg.Backend,
			Options: []string{"", "claude", "codex", "opencode", "opencode-acp"},
		},
		{Label: "Model", Key: "model", Kind: FormFieldText, Value: scfg.Model},
		{Label: "Skill", Key: "skill", Kind: FormFieldText, Value: scfg.Skill},
		{
			Label: "Mode", Key: "mode", Kind: FormFieldCycle, Value: mode,
			Options: []string{"", "plan", "act"},
		},
		{Label: "Gate", Key: "gate", Kind: FormFieldToggle, BoolValue: scfg.Gate},
		{Label: "Description", Key: "description", Kind: FormFieldTextArea, Value: scfg.Description},
		{Label: "Steering", Key: "steering", Kind: FormFieldTextArea, Value: scfg.Steering},
		{Label: "Requires", Key: "requires", Kind: FormFieldText, Value: strings.Join(scfg.Requires, ", ")},
		{Label: "AfterDone", Key: "after_done", Kind: FormFieldText, Value: strings.Join(scfg.AfterDone, ", ")},
	}

	s.form = NewForm(s.com.Styles, fields)
	s.mode = stationsModeEdit
	return nil
}

func (s *Stations) saveStation() Action {
	vals := s.form.Values()
	cfg := s.com.Config()
	name := s.editName
	targetScope := config.ConfigScope(vals["scope"].Value)

	// Apply form values onto editCfg (preserves ArtifactType, Env, etc.)
	ec := s.editCfg
	ec.Backend = vals["backend"].Value
	ec.Model = vals["model"].Value
	ec.Skill = vals["skill"].Value
	ec.Gate = vals["gate"].BoolValue
	ec.Description = vals["description"].Value
	ec.Steering = vals["steering"].Value

	ec.Requires = splitTrimmed(vals["requires"].Value)
	ec.AfterDone = splitTrimmed(vals["after_done"].Value)

	if err := validateStationRoutes(name, ec.Requires, ec.AfterDone, cfg.Stations); err != nil {
		return ActionCmd{Cmd: util.ReportError(err)}
	}

	if modeVal := vals["mode"].Value; modeVal != "" {
		if ec.Options == nil {
			ec.Options = make(map[string]string)
		}
		ec.Options["mode"] = modeVal
	} else if ec.Options != nil {
		delete(ec.Options, "mode")
	}

	// If scope changed from a writable scope, remove from old location.
	oldScope := cfg.StationScope(name)
	if oldScope != config.ConfigScopeDefault && oldScope != targetScope {
		if err := cfg.RemoveScopedConfigField("stations."+name, oldScope); err != nil {
			return ActionCmd{Cmd: util.ReportError(err)}
		}
	}

	// Write to target scope.
	if err := cfg.SetScopedConfigField("stations."+name, ec, targetScope); err != nil {
		return ActionCmd{Cmd: util.ReportError(err)}
	}

	cfg.Stations[name] = ec
	s.mode = stationsModeList
	s.buildList()
	return ActionReloadStations{Stations: []string{name}}
}

func (s *Stations) toggleDisabled() Action {
	item, ok := s.list.SelectedItem().(*StationItem)
	if !ok {
		return nil
	}
	cfg := s.com.Config()
	scfg := cfg.Stations[item.name]
	scfg.Disabled = !scfg.Disabled

	scope := cfg.StationScope(item.name)
	if scope == config.ConfigScopeDefault || scope == config.ConfigScopeUser {
		scope = config.ConfigScopeGlobal
	}
	if err := cfg.SetScopedConfigField("stations."+item.name+".disabled", scfg.Disabled, scope); err != nil {
		return ActionCmd{Cmd: util.ReportError(err)}
	}

	cfg.Stations[item.name] = scfg
	s.buildList()
	return ActionReloadStations{Stations: []string{item.name}}
}

func (s *Stations) resetStation() Action {
	item, ok := s.list.SelectedItem().(*StationItem)
	if !ok {
		return nil
	}
	defCfg, isBuiltin := config.DefaultStations[item.name]
	if !isBuiltin {
		return nil
	}
	cfg := s.com.Config()

	// Remove from all writable scopes.
	for _, scope := range []config.ConfigScope{config.ConfigScopeGlobal, config.ConfigScopeProject} {
		if err := cfg.RemoveScopedConfigField("stations."+item.name, scope); err != nil {
			return ActionCmd{Cmd: util.ReportError(err)}
		}
	}

	cfg.Stations[item.name] = defCfg
	s.buildList()
	return ActionReloadStations{Stations: []string{item.name}}
}

func (s *Stations) deleteStation() Action {
	item, ok := s.list.SelectedItem().(*StationItem)
	if !ok {
		return nil
	}
	if item.isBuiltin {
		return nil
	}
	cfg := s.com.Config()
	scope := cfg.StationScope(item.name)
	if scope == config.ConfigScopeDefault || scope == config.ConfigScopeUser {
		scope = config.ConfigScopeGlobal
	}
	if err := cfg.RemoveScopedConfigField("stations."+item.name, scope); err != nil {
		return ActionCmd{Cmd: util.ReportError(err)}
	}
	delete(cfg.Stations, item.name)
	s.buildList()
	return ActionReloadStations{Stations: []string{item.name}}
}

// validateStationRoutes checks that Requires and AfterDone references are valid:
// all referenced stations must exist, no self-references, and no cycles in Requires.
func validateStationRoutes(name string, requires, afterDone []string, stations map[string]config.StationConfig) error {
	if err := validateStationRefs(name, "requires", requires, stations); err != nil {
		return err
	}
	if err := validateStationRefs(name, "afterDone", afterDone, stations); err != nil {
		return err
	}
	return detectRequiresCycle(name, requires, stations)
}

// validateStationRefs checks that all refs exist and none reference the station itself.
func validateStationRefs(name, field string, refs []string, stations map[string]config.StationConfig) error {
	for _, ref := range refs {
		if ref == name {
			return fmt.Errorf("station %q cannot reference itself in %s", name, field)
		}
		if _, ok := stations[ref]; !ok {
			return fmt.Errorf("%s: unknown station %q", field, ref)
		}
	}
	return nil
}

// detectRequiresCycle checks for circular dependencies in the Requires graph
// starting from the station being edited.
func detectRequiresCycle(name string, requires []string, stations map[string]config.StationConfig) error {
	// Build a temporary requires graph including the edit being saved.
	reqGraph := make(map[string][]string, len(stations))
	for sn, sc := range stations {
		reqGraph[sn] = sc.Requires
	}
	reqGraph[name] = requires

	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	if hasCycleDFS(name, reqGraph, visited, inStack) {
		return fmt.Errorf("circular dependency detected in requires for station %q", name)
	}
	return nil
}

func hasCycleDFS(node string, graph map[string][]string, visited, inStack map[string]bool) bool {
	visited[node] = true
	inStack[node] = true
	for _, dep := range graph[node] {
		if inStack[dep] {
			return true
		}
		if !visited[dep] && hasCycleDFS(dep, graph, visited, inStack) {
			return true
		}
	}
	inStack[node] = false
	return false
}

// splitTrimmed splits a comma-separated string into trimmed, non-empty parts.
func splitTrimmed(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Draw renders the Stations dialog.
func (s *Stations) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	width := min(stationsDialogMaxWidth, area.Dx())
	height := min(stationsDialogMaxHeight, area.Dy())
	t := s.com.Styles

	rc := NewRenderContext(t, width)
	rc.Gap = 1
	rc.Help = s.help.View(s)

	if s.mode == stationsModeEdit {
		rc.Title = "Edit Station: " + s.editName
		innerW := width - t.Dialog.View.GetHorizontalFrameSize()
		rc.AddPart(s.form.Render(innerW))
	} else {
		rc.Title = "Stations"
		s.list.SetSize(width-t.Dialog.View.GetHorizontalFrameSize(), height)
		rc.AddPart(s.list.Render())
	}

	view := rc.Render()
	var cur *tea.Cursor
	if s.mode == stationsModeEdit {
		cur = s.form.Cursor()
	}
	if cur != nil {
		DrawCenterCursor(scr, area, view, cur)
	} else {
		DrawCenter(scr, area, view)
	}
	return cur
}
