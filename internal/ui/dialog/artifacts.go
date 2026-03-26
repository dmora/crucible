package dialog

import (
	"context"
	"image"
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
	"google.golang.org/adk/artifact"
)

// ArtifactsID is the identifier for the artifact viewer dialog.
const ArtifactsID = "artifacts"

// ADK identity constants (duplicated from agent.go — stable, see design Assumption A5).
const (
	adkAppName = "crucible"
	adkUserID  = "user"
)

type artifactsMode uint8

const (
	artifactsModeList artifactsMode = iota
	artifactsModeView
)

// Artifacts is a two-mode dialog for viewing station artifacts.
type Artifacts struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model
	mode  artifactsMode

	entries []ArtifactEntry
	errMsg  string

	// View mode state.
	viewContent    string
	viewRawContent string // raw markdown for clipboard copy
	viewTitle      string
	viewHeaderBar  string // styled header bar for view mode
	viewScroll     int
	viewHeight     int
	viewLines      []string

	// Mouse selection state (view mode only).
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

var _ Dialog = (*Artifacts)(nil)

// NewArtifacts creates a new Artifacts dialog, loading artifacts for the given session.
func NewArtifacts(com *common.Common, artifactSvc artifact.Service, sessionID string) *Artifacts {
	a := new(Artifacts)
	a.com = com
	a.mode = artifactsModeList

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	a.help = h

	a.input = textinput.New()
	a.input.SetVirtualCursor(false)
	a.input.Placeholder = "Filter artifacts"
	a.input.SetStyles(com.Styles.TextInput)
	a.input.Focus()

	a.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "view"),
	)
	a.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	a.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous"),
	)
	a.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "navigate"),
	)
	a.keyMap.Back = key.NewBinding(
		key.WithKeys("backspace"),
		key.WithHelp("bksp", "back"),
	)
	a.keyMap.Close = CloseKey
	a.keyMap.ScrollDown = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "scroll down"),
	)
	a.keyMap.ScrollUp = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "scroll up"),
	)
	a.keyMap.PageDown = key.NewBinding(
		key.WithKeys("pgdown", "f", " "),
		key.WithHelp("f/pgdn", "page down"),
	)
	a.keyMap.PageUp = key.NewBinding(
		key.WithKeys("pgup", "b"),
		key.WithHelp("b/pgup", "page up"),
	)
	a.keyMap.Copy = key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "copy"),
	)

	a.entries, a.errMsg = loadArtifacts(artifactSvc, sessionID)
	a.list = list.NewFilterableList(artifactItems(com.Styles, a.entries)...)
	a.list.Focus()
	return a
}

// parseArtifactName extracts station and type from an artifact name.
// Expected format: "station-{name}-{type}" (e.g., "station-plan-spec").
func parseArtifactName(name string) (station, artifactType string) {
	rest, found := strings.CutPrefix(name, "station-")
	if !found {
		return "", name
	}
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

// ID implements Dialog.
func (a *Artifacts) ID() string {
	return ArtifactsID
}

// loadArtifacts fetches all artifacts for a session from the artifact service.
func loadArtifacts(svc artifact.Service, sessionID string) ([]ArtifactEntry, string) {
	if svc == nil || sessionID == "" {
		return nil, ""
	}
	ctx := context.TODO()
	listResp, err := svc.List(ctx, &artifact.ListRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err.Error()
	}
	var entries []ArtifactEntry
	for _, filename := range listResp.FileNames {
		loadResp, loadErr := svc.Load(ctx, &artifact.LoadRequest{
			AppName:   adkAppName,
			UserID:    adkUserID,
			SessionID: sessionID,
			FileName:  filename,
			Version:   0,
		})
		if loadErr != nil || loadResp.Part == nil {
			continue
		}
		station, artifactType := parseArtifactName(filename)
		entries = append(entries, ArtifactEntry{
			Name:    filename,
			Station: station,
			Type:    artifactType,
			Content: loadResp.Part.Text,
		})
	}
	return entries, ""
}

// HandleMsg implements Dialog.
func (a *Artifacts) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch a.mode {
		case artifactsModeList:
			return a.handleListKey(msg)
		case artifactsModeView:
			return a.handleViewKey(msg)
		}
	case tea.MouseClickMsg:
		if a.mode == artifactsModeView {
			return a.handleViewMouseDown(msg)
		}
	case tea.MouseMotionMsg:
		if a.mode == artifactsModeView {
			a.handleViewMouseDrag(msg)
		}
	case tea.MouseReleaseMsg:
		if a.mode == artifactsModeView {
			return a.handleViewMouseUp(msg)
		}
	}
	return nil
}

func (a *Artifacts) handleListKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, a.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, a.keyMap.Previous):
		a.list.Focus()
		if a.list.IsSelectedFirst() {
			a.list.SelectLast()
		} else {
			a.list.SelectPrev()
		}
		a.list.ScrollToSelected()
	case key.Matches(msg, a.keyMap.Next):
		a.list.Focus()
		if a.list.IsSelectedLast() {
			a.list.SelectFirst()
		} else {
			a.list.SelectNext()
		}
		a.list.ScrollToSelected()
	case key.Matches(msg, a.keyMap.Select):
		item := a.list.SelectedItem()
		if item == nil {
			return nil
		}
		ai, ok := item.(*ArtifactItem)
		if !ok {
			return nil
		}
		a.enterViewMode(ai.entry)
	default:
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		a.list.SetFilter(a.input.Value())
		a.list.ScrollToTop()
		a.list.SetSelected(0)
		return ActionCmd{cmd}
	}
	return nil
}

func (a *Artifacts) handleViewKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, a.keyMap.Copy):
		cmd := common.CopyToClipboard(a.viewRawContent, "Artifact copied to clipboard")
		return ActionCmd{cmd}
	case key.Matches(msg, a.keyMap.Back), key.Matches(msg, a.keyMap.Close):
		a.clearSelection()
		a.mode = artifactsModeList
	case key.Matches(msg, a.keyMap.ScrollDown):
		a.clearSelection()
		a.scrollBy(1)
	case key.Matches(msg, a.keyMap.ScrollUp):
		a.clearSelection()
		a.scrollBy(-1)
	case key.Matches(msg, a.keyMap.PageDown):
		a.clearSelection()
		a.scrollBy(a.viewHeight)
	case key.Matches(msg, a.keyMap.PageUp):
		a.clearSelection()
		a.scrollBy(-a.viewHeight)
	}
	return nil
}

func (a *Artifacts) handleViewMouseDown(msg tea.MouseClickMsg) Action {
	if !image.Pt(msg.X, msg.Y).In(a.contentArea) {
		return nil
	}
	a.mouseDown = true
	a.mouseStartX = msg.X - a.contentArea.Min.X
	a.mouseStartY = msg.Y - a.contentArea.Min.Y
	a.mouseDragX = a.mouseStartX
	a.mouseDragY = a.mouseStartY
	return nil
}

func (a *Artifacts) handleViewMouseDrag(msg tea.MouseMotionMsg) {
	if !a.mouseDown {
		return
	}
	a.mouseDragX = max(0, min(msg.X-a.contentArea.Min.X, a.contentArea.Dx()-1))
	a.mouseDragY = max(0, min(msg.Y-a.contentArea.Min.Y, a.contentArea.Dy()-1))
}

func (a *Artifacts) handleViewMouseUp(_ tea.MouseReleaseMsg) Action {
	if !a.mouseDown {
		return nil
	}
	a.mouseDown = false

	sLine, sCol, eLine, eCol, ok := a.normalizedSelection()
	if !ok {
		return nil
	}

	end := min(a.viewScroll+a.viewHeight, len(a.viewLines))
	start := min(a.viewScroll, end)
	visible := strings.Join(a.viewLines[start:end], "\n")
	text := list.HighlightContent(visible,
		image.Rect(0, 0, a.contentArea.Dx(), a.contentArea.Dy()),
		sLine, sCol, eLine, eCol)
	text = strings.TrimRight(text, "\n")

	if text == "" {
		a.clearSelection()
		return nil
	}

	cmd := common.CopyToClipboardWithCallback(text, "Copied to clipboard", func() tea.Msg {
		a.clearSelection()
		return nil
	})
	return ActionCmd{cmd}
}

func (a *Artifacts) normalizedSelection() (int, int, int, int, bool) {
	sY, sX := a.mouseStartY, a.mouseStartX
	eY, eX := a.mouseDragY, a.mouseDragX
	if sY == eY && sX == eX {
		return 0, 0, 0, 0, false
	}
	if sY > eY || (sY == eY && sX > eX) {
		sY, sX, eY, eX = eY, eX, sY, sX
	}
	return sY, sX, eY, eX, true
}

func (a *Artifacts) clearSelection() {
	a.mouseDown = false
	a.mouseStartX = 0
	a.mouseStartY = 0
	a.mouseDragX = 0
	a.mouseDragY = 0
}

func (a *Artifacts) enterViewMode(entry ArtifactEntry) {
	a.mode = artifactsModeView
	a.viewScroll = 0
	a.viewTitle = "Artifacts"
	a.viewRawContent = entry.Content

	// Build station-card-style header bar: [TYPE] chip · station · artifact name
	t := a.com.Styles
	sep := t.Muted.Render(" · ")
	if entry.Type != "" && entry.Station != "" {
		chip := t.TagInfo.Bold(true).Render(strings.ToUpper(entry.Type))
		station := t.Tool.NameNormal.Render(entry.Station)
		name := t.Subtle.Render(entry.Name)
		a.viewHeaderBar = chip + sep + station + sep + name
	} else {
		a.viewHeaderBar = t.Tool.NameNormal.Render(entry.Name)
	}

	// Render markdown content.
	width := defaultDialogMaxWidth - 6 // account for dialog frame padding
	r := common.MarkdownRenderer(t, width)
	if r != nil {
		rendered, err := r.Render(entry.Content)
		if err == nil {
			a.viewContent = rendered
		} else {
			a.viewContent = entry.Content
		}
	} else {
		a.viewContent = entry.Content
	}
	a.viewLines = strings.Split(a.viewContent, "\n")
}

func (a *Artifacts) scrollBy(delta int) {
	a.viewScroll += delta
	maxScroll := max(0, len(a.viewLines)-a.viewHeight)
	a.viewScroll = max(0, min(a.viewScroll, maxScroll))
}

func (a *Artifacts) drawViewMode(scr uv.Screen, area uv.Rectangle, rc *RenderContext, t *styles.Styles, height, innerWidth int) {
	rc.Title = a.viewTitle
	headerBarHeight := 1
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		headerBarHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	a.viewHeight = max(1, height-heightOffset)
	a.help.SetWidth(innerWidth)

	rc.AddPart(a.viewHeaderBar)

	// Render visible slice of content.
	end := min(a.viewScroll+a.viewHeight, len(a.viewLines))
	start := min(a.viewScroll, end)
	visible := strings.Join(a.viewLines[start:end], "\n")

	// Apply mouse selection highlight if active.
	if sLine, sCol, eLine, eCol, ok := a.normalizedSelection(); ok {
		visible = list.Highlight(visible,
			image.Rect(0, 0, innerWidth, a.viewHeight),
			sLine, sCol, eLine, eCol,
			list.DefaultHighlighter)
	}

	contentView := lipgloss.NewStyle().
		Width(innerWidth).
		Height(a.viewHeight).
		Render(visible)
	rc.AddPart(contentView)

	rc.Help = a.help.View(a)
	view := rc.Render()
	DrawCenter(scr, area, view)
	a.cacheContentArea(t, area, view, innerWidth, headerBarHeight)
}

func (a *Artifacts) cacheContentArea(t *styles.Styles, area uv.Rectangle, view string, innerWidth, headerBarHeight int) {
	dialogW, dialogH := lipgloss.Size(view)
	center := common.CenterRect(area, dialogW, dialogH)
	contentTop := center.Min.Y +
		t.Dialog.View.GetBorderTopSize() +
		t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		headerBarHeight
	contentLeft := center.Min.X +
		t.Dialog.View.GetBorderLeftSize()
	a.contentArea = image.Rect(contentLeft, contentTop, contentLeft+innerWidth, contentTop+a.viewHeight)
}

// Draw implements Dialog.
func (a *Artifacts) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := a.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)

	switch a.mode {
	case artifactsModeList:
		rc.Title = "Artifacts"
		heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
			t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
			t.Dialog.HelpView.GetVerticalFrameSize() +
			t.Dialog.View.GetVerticalFrameSize()

		a.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
		a.list.SetSize(innerWidth, height-heightOffset)
		a.help.SetWidth(innerWidth)

		inputView := t.Dialog.InputPrompt.Render(a.input.View())
		rc.AddPart(inputView)

		if a.errMsg != "" {
			errView := lipgloss.NewStyle().
				Foreground(t.TagError.GetBackground()).
				Width(innerWidth).
				Render("Error: " + a.errMsg)
			rc.AddPart(errView)
		} else if len(a.entries) == 0 {
			emptyView := lipgloss.NewStyle().
				Foreground(t.Subtle.GetForeground()).
				Width(innerWidth).
				Render("No artifacts in this session")
			rc.AddPart(emptyView)
		} else {
			listView := t.Dialog.List.Height(a.list.Height()).Render(a.list.Render())
			rc.AddPart(listView)
		}

		rc.Help = a.help.View(a)
		view := rc.Render()
		cur := InputCursor(t, a.input.Cursor())
		DrawCenterCursor(scr, area, view, cur)
		return cur

	case artifactsModeView:
		a.drawViewMode(scr, area, rc, t, height, innerWidth)
		return nil
	}

	return nil
}

// ShortHelp implements help.KeyMap.
func (a *Artifacts) ShortHelp() []key.Binding {
	switch a.mode {
	case artifactsModeView:
		return []key.Binding{
			a.keyMap.ScrollUp,
			a.keyMap.ScrollDown,
			a.keyMap.PageUp,
			a.keyMap.PageDown,
			a.keyMap.Copy,
			a.keyMap.Back,
			a.keyMap.Close,
		}
	default:
		return []key.Binding{
			a.keyMap.UpDown,
			a.keyMap.Select,
			a.keyMap.Close,
		}
	}
}

// FullHelp implements help.KeyMap.
func (a *Artifacts) FullHelp() [][]key.Binding {
	bindings := a.ShortHelp()
	var rows [][]key.Binding
	for i := 0; i < len(bindings); i += 4 {
		end := min(i+4, len(bindings))
		rows = append(rows, bindings[i:end])
	}
	return rows
}
