package dialog

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

const (
	RelayDialogID        = "relay"
	relayDialogMaxWidth  = 50
	relayDialogMaxHeight = 14
)

// Relay represents a dialog for selecting a relay station target.
type Relay struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// RelayItem represents a station in the relay picker.
type RelayItem struct {
	station   string // station name (empty = "supervisor")
	title     string
	desc      string
	isCurrent bool
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var (
	_ Dialog   = (*Relay)(nil)
	_ ListItem = (*RelayItem)(nil)
)

// NewRelay creates a new relay station picker dialog.
func NewRelay(com *common.Common, stations map[string]config.StationConfig, currentTarget *string) *Relay {
	r := &Relay{com: com}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	r.help = h

	r.list = list.NewFilterableList()
	r.list.Focus()

	r.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	)
	r.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	r.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous"),
	)
	r.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	r.keyMap.Close = CloseKey

	r.setItems(stations, currentTarget)
	return r
}

// ID implements Dialog.
func (r *Relay) ID() string {
	return RelayDialogID
}

// HandleMsg implements Dialog.
func (r *Relay) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, r.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, r.keyMap.Previous):
			r.list.Focus()
			if r.list.IsSelectedFirst() {
				r.list.SelectLast()
				r.list.ScrollToBottom()
				break
			}
			r.list.SelectPrev()
			r.list.ScrollToSelected()
		case key.Matches(msg, r.keyMap.Next):
			r.list.Focus()
			if r.list.IsSelectedLast() {
				r.list.SelectFirst()
				r.list.ScrollToTop()
				break
			}
			r.list.SelectNext()
			r.list.ScrollToSelected()
		case key.Matches(msg, r.keyMap.Select):
			selected := r.list.SelectedItem()
			if selected == nil {
				break
			}
			item, ok := selected.(*RelayItem)
			if !ok {
				break
			}
			return ActionSelectRelay{Station: item.station}
		}
	}
	return nil
}

// Draw implements Dialog.
func (r *Relay) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := r.com.Styles
	width := max(0, min(relayDialogMaxWidth, area.Dx()))
	height := max(0, min(relayDialogMaxHeight, area.Dy()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	r.list.SetSize(innerWidth, height-heightOffset)
	r.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "RELAY TARGET"

	visibleCount := len(r.list.FilteredItems())
	if r.list.Height() >= visibleCount {
		r.list.ScrollToTop()
	} else {
		r.list.ScrollToSelected()
	}

	listView := t.Dialog.List.Height(r.list.Height()).Render(r.list.Render())
	rc.AddPart(listView)
	rc.Help = r.help.View(r)

	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

// ShortHelp implements help.KeyMap.
func (r *Relay) ShortHelp() []key.Binding {
	return []key.Binding{
		r.keyMap.UpDown,
		r.keyMap.Select,
		r.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (r *Relay) FullHelp() [][]key.Binding {
	return [][]key.Binding{{
		r.keyMap.Select,
		r.keyMap.Next,
		r.keyMap.Previous,
		r.keyMap.Close,
	}}
}

func (r *Relay) setItems(stations map[string]config.StationConfig, currentTarget *string) {
	// Collect and sort station names.
	names := make([]string, 0, len(stations))
	for name, cfg := range stations {
		if !cfg.Disabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	items := make([]list.FilterableItem, 0, len(names)+1)
	selectedIndex := 0

	// Add "SUPERVISOR" option when relay is active.
	if currentTarget != nil {
		items = append(items, &RelayItem{
			station: "", // empty = back to supervisor
			title:   "SUPERVISOR",
			desc:    "Return to supervisor",
			t:       r.com.Styles,
		})
	}

	for _, name := range names {
		cfg := stations[name]
		isCurrent := currentTarget != nil && *currentTarget == name
		item := &RelayItem{
			station:   name,
			title:     strings.ToUpper(name),
			desc:      cfg.Description,
			isCurrent: isCurrent,
			t:         r.com.Styles,
		}
		items = append(items, item)
		if isCurrent {
			selectedIndex = len(items) - 1
		}
	}

	r.list.SetItems(items...)
	r.list.SetSelected(selectedIndex)
}

// Filter returns the filter value for the relay item.
func (r *RelayItem) Filter() string {
	return r.title
}

// ID returns the unique identifier.
func (r *RelayItem) ID() string {
	if r.station == "" {
		return "supervisor"
	}
	return r.station
}

// SetFocused sets the focus state.
func (r *RelayItem) SetFocused(focused bool) {
	if r.focused != focused {
		r.cache = nil
	}
	r.focused = focused
}

// SetMatch sets the fuzzy match.
func (r *RelayItem) SetMatch(m fuzzy.Match) {
	r.cache = nil
	r.m = m
}

// Render returns the string representation.
func (r *RelayItem) Render(width int) string {
	info := ""
	if r.isCurrent {
		info = "active"
	}
	s := ListItemStyles{
		ItemBlurred:     r.t.Dialog.NormalItem,
		ItemFocused:     r.t.Dialog.SelectedItem,
		InfoTextBlurred: r.t.Base,
		InfoTextFocused: r.t.Base,
	}
	return renderItem(s, r.title, info, r.focused, width, r.cache, &r.m)
}
