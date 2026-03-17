package dialog

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

const (
	ThemeDialogID        = "theme"
	themeDialogMaxWidth  = 50
	themeDialogMaxHeight = 16
)

// themeDisplayNames maps theme IDs to human-readable names.
var themeDisplayNames = map[styles.ThemeID]string{
	styles.ThemeSteelBlue:     "Steel Blue",
	styles.ThemeAmberForge:    "Amber Forge",
	styles.ThemePhosphorGreen: "Phosphor Green",
	styles.ThemeReactorRed:    "Reactor Red",
	styles.ThemeTitanium:      "Titanium",
	styles.ThemeCleanRoom:     "Clean Room",
}

// Theme represents a dialog for selecting a UI theme.
type Theme struct {
	com           *common.Common
	help          help.Model
	list          *list.FilterableList
	input         textinput.Model
	isTransparent bool

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Toggle   key.Binding
		Close    key.Binding
	}
}

// ThemeItem represents a theme list item.
type ThemeItem struct {
	themeID   string
	title     string
	swatch    string
	isCurrent bool
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var (
	_ Dialog   = (*Theme)(nil)
	_ ListItem = (*ThemeItem)(nil)
)

// NewTheme creates a new theme selection dialog.
func NewTheme(com *common.Common) (*Theme, error) {
	t := &Theme{com: com}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	t.help = h

	t.list = list.NewFilterableList()
	t.list.Focus()

	t.input = textinput.New()
	t.input.SetVirtualCursor(false)
	t.input.Placeholder = "Type to filter"
	t.input.SetStyles(com.Styles.TextInput)
	t.input.Focus()

	t.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	t.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	t.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	t.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	t.keyMap.Toggle = key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "transparent"),
	)
	t.keyMap.Close = CloseKey

	t.isTransparent = com.Config().IsTransparent()
	t.setThemeItems()

	return t, nil
}

// ID implements Dialog.
func (t *Theme) ID() string {
	return ThemeDialogID
}

// HandleMsg implements [Dialog].
func (t *Theme) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if key.Matches(keyMsg, t.keyMap.Close) {
		return ActionClose{}
	}
	if key.Matches(keyMsg, t.keyMap.Toggle) {
		t.isTransparent = !t.isTransparent
		return ActionSetTransparent{Transparent: t.isTransparent}
	}
	if action := t.handleNavigation(keyMsg); action != nil {
		return action
	}
	if key.Matches(keyMsg, t.keyMap.Select) {
		return t.handleSelect()
	}
	return t.handleFilterInput(keyMsg)
}

func (t *Theme) handleNavigation(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, t.keyMap.Previous):
		t.list.Focus()
		if t.list.IsSelectedFirst() {
			t.list.SelectLast()
			t.list.ScrollToBottom()
		} else {
			t.list.SelectPrev()
			t.list.ScrollToSelected()
		}
		return ActionCmd{} // consumed
	case key.Matches(msg, t.keyMap.Next):
		t.list.Focus()
		if t.list.IsSelectedLast() {
			t.list.SelectFirst()
			t.list.ScrollToTop()
		} else {
			t.list.SelectNext()
			t.list.ScrollToSelected()
		}
		return ActionCmd{} // consumed
	}
	return nil
}

func (t *Theme) handleSelect() Action {
	if item := t.list.SelectedItem(); item != nil {
		if themeItem, ok := item.(*ThemeItem); ok {
			return ActionSelectTheme{ThemeID: themeItem.themeID}
		}
	}
	return nil
}

func (t *Theme) handleFilterInput(msg tea.KeyPressMsg) Action {
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	t.list.SetFilter(t.input.Value())
	t.list.ScrollToTop()
	t.list.SetSelected(0)
	return ActionCmd{cmd}
}

// Cursor returns the cursor position relative to the dialog.
func (t *Theme) Cursor() *tea.Cursor {
	return InputCursor(t.com.Styles, t.input.Cursor())
}

// Draw implements [Dialog].
func (t *Theme) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	s := t.com.Styles
	width := max(0, min(themeDialogMaxWidth, area.Dx()))
	height := max(0, min(themeDialogMaxHeight, area.Dy()))

	rc := t.buildRenderContext(s, width, height)
	view := rc.Render()

	cur := t.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// buildRenderContext prepares the dialog's render context with input, list, and help.
func (t *Theme) buildRenderContext(s *styles.Styles, width, height int) *RenderContext {
	innerWidth := width - s.Dialog.View.GetHorizontalFrameSize()
	heightOffset := s.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		s.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		s.Dialog.HelpView.GetVerticalFrameSize() +
		s.Dialog.View.GetVerticalFrameSize()

	t.input.SetWidth(innerWidth - s.Dialog.InputPrompt.GetHorizontalFrameSize() - 1)
	t.list.SetSize(innerWidth, height-heightOffset)
	t.help.SetWidth(innerWidth)

	rc := NewRenderContext(s, width)
	rc.Title = "Switch Theme"
	rc.AddPart(s.Dialog.InputPrompt.Render(t.input.View()))

	if t.list.Height() >= len(t.list.FilteredItems()) {
		t.list.ScrollToTop()
	} else {
		t.list.ScrollToSelected()
	}

	rc.AddPart(s.Dialog.List.Height(t.list.Height()).Render(t.list.Render()))

	toggleLabel := "○ Transparent"
	if t.isTransparent {
		toggleLabel = "◉ Transparent"
	}
	rc.AddPart(s.Dialog.InputPrompt.Render(toggleLabel))

	rc.Help = t.help.View(t)
	return rc
}

// ShortHelp implements [help.KeyMap].
func (t *Theme) ShortHelp() []key.Binding {
	return []key.Binding{
		t.keyMap.UpDown,
		t.keyMap.Select,
		t.keyMap.Toggle,
		t.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (t *Theme) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{t.keyMap.Select, t.keyMap.Next, t.keyMap.Previous, t.keyMap.Toggle, t.keyMap.Close},
	}
}

func (t *Theme) setThemeItems() {
	currentThemeID := t.com.Config().ThemeID()
	themeIDs := styles.BuiltinThemeIDs()

	items := make([]list.FilterableItem, 0, len(themeIDs))
	selectedIndex := 0
	for i, id := range themeIDs {
		title := themeDisplayNames[id]
		if title == "" {
			title = string(id)
		}

		var swatch string
		if pal, err := styles.LookupPalette(id); err == nil {
			swatch = swatchFromColor(pal.Primary)
		}

		item := &ThemeItem{
			themeID:   string(id),
			title:     title,
			swatch:    swatch,
			isCurrent: string(id) == currentThemeID,
			t:         t.com.Styles,
		}
		items = append(items, item)
		if string(id) == currentThemeID {
			selectedIndex = i
		}
	}

	t.list.SetItems(items...)
	t.list.SetSelected(selectedIndex)
	t.list.ScrollToSelected()
}

// swatchFromColor renders a small color swatch block from an RGBA color.
func swatchFromColor(c color.RGBA) string {
	hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("██")
}

// Filter returns the filter value for the theme item.
func (ti *ThemeItem) Filter() string {
	return ti.title
}

// ID returns the unique identifier for the theme item.
func (ti *ThemeItem) ID() string {
	return ti.themeID
}

// SetFocused sets the focus state of the theme item.
func (ti *ThemeItem) SetFocused(focused bool) {
	if ti.focused != focused {
		ti.cache = nil
	}
	ti.focused = focused
}

// SetMatch sets the fuzzy match for the theme item.
func (ti *ThemeItem) SetMatch(m fuzzy.Match) {
	ti.cache = nil
	ti.m = m
}

// Render returns the string representation of the theme item.
func (ti *ThemeItem) Render(width int) string {
	info := ti.swatch
	if ti.isCurrent {
		info = ti.swatch + " current"
	}
	s := ListItemStyles{
		ItemBlurred:     ti.t.Dialog.NormalItem,
		ItemFocused:     ti.t.Dialog.SelectedItem,
		InfoTextBlurred: ti.t.Base,
		InfoTextFocused: ti.t.Base,
	}
	return renderItem(s, ti.title, info, ti.focused, width, ti.cache, &ti.m)
}

// ThemeDisplayName returns the human-readable display name for a theme ID.
func ThemeDisplayName(id string) string {
	name := themeDisplayNames[styles.ThemeID(id)]
	if name == "" {
		// Fallback: split on hyphens and title-case
		parts := strings.Split(id, "-")
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}
	return name
}
