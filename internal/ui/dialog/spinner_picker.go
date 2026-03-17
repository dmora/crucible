package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/ui/anim"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

const (
	SpinnerDialogID        = "spinner"
	spinnerDialogMaxWidth  = 50
	spinnerDialogMaxHeight = 12
)

// spinnerDisplayNames maps preset IDs to human-readable names.
var spinnerDisplayNames = map[string]string{
	"industrial": "Industrial",
	"pulse":      "Pulse",
	"dots":       "Dots",
	"ellipsis":   "Ellipsis",
	"points":     "Points",
	"meter":      "Meter",
}

// spinnerDescriptions maps preset IDs to short descriptions.
var spinnerDescriptions = map[string]string{
	"industrial": "Multi-char gradient scramble",
	"pulse":      "Fading block pulse",
	"dots":       "Braille rotation",
	"ellipsis":   "Growing dots",
	"points":     "Traveling dot",
	"meter":      "Fill bar",
}

// SpinnerPicker represents a dialog for selecting a spinner preset.
type SpinnerPicker struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// SpinnerItem represents a spinner list item.
type SpinnerItem struct {
	preset      string
	title       string
	description string
	isCurrent   bool
	t           *styles.Styles
	m           fuzzy.Match
	cache       map[int]string
	focused     bool
}

var (
	_ Dialog   = (*SpinnerPicker)(nil)
	_ ListItem = (*SpinnerItem)(nil)
)

// NewSpinnerPicker creates a new spinner selection dialog.
func NewSpinnerPicker(com *common.Common) (*SpinnerPicker, error) {
	sp := &SpinnerPicker{com: com}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	sp.help = h

	sp.list = list.NewFilterableList()
	sp.list.Focus()

	sp.input = textinput.New()
	sp.input.SetVirtualCursor(false)
	sp.input.Placeholder = "Type to filter"
	sp.input.SetStyles(com.Styles.TextInput)
	sp.input.Focus()

	sp.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	)
	sp.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	sp.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	sp.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	sp.keyMap.Close = CloseKey

	sp.setSpinnerItems()

	return sp, nil
}

// ID implements Dialog.
func (sp *SpinnerPicker) ID() string {
	return SpinnerDialogID
}

// HandleMsg implements [Dialog].
func (sp *SpinnerPicker) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if key.Matches(keyMsg, sp.keyMap.Close) {
		return ActionClose{}
	}
	if action := sp.handleNavigation(keyMsg); action != nil {
		return action
	}
	if key.Matches(keyMsg, sp.keyMap.Select) {
		return sp.handleSelect()
	}
	return sp.handleFilterInput(keyMsg)
}

func (sp *SpinnerPicker) handleNavigation(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, sp.keyMap.Previous):
		sp.list.Focus()
		if sp.list.IsSelectedFirst() {
			sp.list.SelectLast()
			sp.list.ScrollToBottom()
		} else {
			sp.list.SelectPrev()
			sp.list.ScrollToSelected()
		}
		return ActionCmd{} // consumed
	case key.Matches(msg, sp.keyMap.Next):
		sp.list.Focus()
		if sp.list.IsSelectedLast() {
			sp.list.SelectFirst()
			sp.list.ScrollToTop()
		} else {
			sp.list.SelectNext()
			sp.list.ScrollToSelected()
		}
		return ActionCmd{} // consumed
	}
	return nil
}

func (sp *SpinnerPicker) handleSelect() Action {
	if item := sp.list.SelectedItem(); item != nil {
		if spinnerItem, ok := item.(*SpinnerItem); ok {
			return ActionSelectSpinner{Preset: spinnerItem.preset}
		}
	}
	return nil
}

func (sp *SpinnerPicker) handleFilterInput(msg tea.KeyPressMsg) Action {
	var cmd tea.Cmd
	sp.input, cmd = sp.input.Update(msg)
	sp.list.SetFilter(sp.input.Value())
	sp.list.ScrollToTop()
	sp.list.SetSelected(0)
	return ActionCmd{cmd}
}

// Cursor returns the cursor position relative to the dialog.
func (sp *SpinnerPicker) Cursor() *tea.Cursor {
	return InputCursor(sp.com.Styles, sp.input.Cursor())
}

// Draw implements [Dialog].
func (sp *SpinnerPicker) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	s := sp.com.Styles
	width := max(0, min(spinnerDialogMaxWidth, area.Dx()))
	height := max(0, min(spinnerDialogMaxHeight, area.Dy()))

	rc := sp.buildRenderContext(s, width, height)
	view := rc.Render()

	cur := sp.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// buildRenderContext prepares the dialog's render context with input, list, and help.
func (sp *SpinnerPicker) buildRenderContext(s *styles.Styles, width, height int) *RenderContext {
	innerWidth := width - s.Dialog.View.GetHorizontalFrameSize()
	heightOffset := s.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		s.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		s.Dialog.HelpView.GetVerticalFrameSize() +
		s.Dialog.View.GetVerticalFrameSize()

	sp.input.SetWidth(innerWidth - s.Dialog.InputPrompt.GetHorizontalFrameSize() - 1)
	sp.list.SetSize(innerWidth, height-heightOffset)
	sp.help.SetWidth(innerWidth)

	rc := NewRenderContext(s, width)
	rc.Title = "Switch Spinner"
	rc.AddPart(s.Dialog.InputPrompt.Render(sp.input.View()))

	if sp.list.Height() >= len(sp.list.FilteredItems()) {
		sp.list.ScrollToTop()
	} else {
		sp.list.ScrollToSelected()
	}

	rc.AddPart(s.Dialog.List.Height(sp.list.Height()).Render(sp.list.Render()))
	rc.Help = sp.help.View(sp)
	return rc
}

// ShortHelp implements [help.KeyMap].
func (sp *SpinnerPicker) ShortHelp() []key.Binding {
	return []key.Binding{
		sp.keyMap.UpDown,
		sp.keyMap.Select,
		sp.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (sp *SpinnerPicker) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{sp.keyMap.Select, sp.keyMap.Next, sp.keyMap.Previous, sp.keyMap.Close},
	}
}

func (sp *SpinnerPicker) setSpinnerItems() {
	currentPreset := sp.com.Config().SpinnerPreset()
	presets := anim.Presets()

	items := make([]list.FilterableItem, 0, len(presets))
	selectedIndex := 0
	for i, preset := range presets {
		title := spinnerDisplayNames[preset]
		if title == "" {
			title = preset
		}
		desc := spinnerDescriptions[preset]

		item := &SpinnerItem{
			preset:      preset,
			title:       title,
			description: desc,
			isCurrent:   preset == currentPreset,
			t:           sp.com.Styles,
		}
		items = append(items, item)
		if preset == currentPreset {
			selectedIndex = i
		}
	}

	sp.list.SetItems(items...)
	sp.list.SetSelected(selectedIndex)
	sp.list.ScrollToSelected()
}

// Filter returns the filter value for the spinner item.
func (si *SpinnerItem) Filter() string {
	return si.title
}

// ID returns the unique identifier for the spinner item.
func (si *SpinnerItem) ID() string {
	return si.preset
}

// SetFocused sets the focus state of the spinner item.
func (si *SpinnerItem) SetFocused(focused bool) {
	if si.focused != focused {
		si.cache = nil
	}
	si.focused = focused
}

// SetMatch sets the fuzzy match for the spinner item.
func (si *SpinnerItem) SetMatch(m fuzzy.Match) {
	si.cache = nil
	si.m = m
}

// Render returns the string representation of the spinner item.
func (si *SpinnerItem) Render(width int) string {
	info := si.description
	if si.isCurrent {
		info += " current"
	}
	s := ListItemStyles{
		ItemBlurred:     si.t.Dialog.NormalItem,
		ItemFocused:     si.t.Dialog.SelectedItem,
		InfoTextBlurred: si.t.Base,
		InfoTextFocused: si.t.Base,
	}
	return renderItem(s, si.title, info, si.focused, width, si.cache, &si.m)
}

// SpinnerDisplayName returns the human-readable display name for a spinner preset.
func SpinnerDisplayName(preset string) string {
	name := spinnerDisplayNames[preset]
	if name == "" {
		// Fallback: split on hyphens and title-case
		parts := strings.Split(preset, "-")
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}
	return name
}
