package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/ui/common"
)

// AskUserID is the identifier for the ask_user dialog.
const AskUserID = "askuser"

// askUserMaxWidth is the maximum dialog width.
const askUserMaxWidth = 90

// otherLabel is the label for the auto-appended "Other..." option.
const otherLabel = "Other..."

// questionType identifies the rendering mode for a question.
type questionType int

const (
	questionTypeSingleSelect questionType = iota
	questionTypeMultiSelect
	questionTypeFreeText
	questionTypeConfirm
)

// questionState tracks per-question UI state.
type questionState struct {
	qType       questionType
	cursor      int             // currently highlighted option
	selected    map[int]bool    // toggled options (multi-select)
	textInput   textinput.Model // for free text and "Other..."
	otherActive bool            // "Other..." is selected and text input visible
	confirmed   *int            // for confirm: 0=Yes, 1=No, nil=not yet
}

// AskUser represents a dialog for structured operator questions.
type AskUser struct {
	com     *common.Common
	request askuser.Request

	questions []questionState
	active    int // currently active question index

	help   help.Model
	keyMap askUserKeyMap
}

type askUserKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Select  key.Binding
	Toggle  key.Binding
	Close   key.Binding
	NavQ    key.Binding // help-only binding for question navigation
	NavOpts key.Binding // help-only binding for option navigation
}

func defaultAskUserKeyMap() askUserKeyMap {
	return askUserKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "prev question"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l", "tab"),
			key.WithHelp("→", "next question"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "next/submit"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("space"),
			key.WithHelp("space", "toggle"),
		),
		Close: CloseKey,
		NavQ: key.NewBinding(
			key.WithKeys("left", "right"),
			key.WithHelp("←/→", "questions"),
		),
		NavOpts: key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("↑/↓", "options"),
		),
	}
}

var _ Dialog = (*AskUser)(nil)

// NewAskUser creates a new ask_user dialog.
func NewAskUser(com *common.Common, req askuser.Request) *AskUser {
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	d := &AskUser{
		com:     com,
		request: req,
		help:    h,
		keyMap:  defaultAskUserKeyMap(),
	}

	d.questions = make([]questionState, len(req.Questions))
	for i, q := range req.Questions {
		d.questions[i] = newQuestionState(com, q)
	}
	// Focus the first question's text input if it's free text.
	if len(d.questions) > 0 && d.questions[0].qType == questionTypeFreeText {
		d.questions[0].textInput.Focus()
	}

	return d
}

func newQuestionState(com *common.Common, q askuser.Question) questionState {
	ti := textinput.New()
	ti.SetVirtualCursor(false)
	ti.SetStyles(com.Styles.TextInput)
	ti.Prompt = "> "
	ti.Placeholder = "Type your answer..."
	ti.Blur()

	qs := questionState{
		selected:  make(map[int]bool),
		textInput: ti,
	}

	switch {
	case len(q.Options) == 0:
		qs.qType = questionTypeFreeText
	case q.MultiSelect:
		qs.qType = questionTypeMultiSelect
	case isConfirmQuestion(q.Options):
		qs.qType = questionTypeConfirm
	default:
		qs.qType = questionTypeSingleSelect
	}

	return qs
}

// isConfirmQuestion detects Yes/No style questions.
func isConfirmQuestion(opts []askuser.Option) bool {
	if len(opts) != 2 {
		return false
	}
	labels := [2]string{strings.ToLower(opts[0].Label), strings.ToLower(opts[1].Label)}
	return (labels[0] == "yes" && labels[1] == "no") ||
		(labels[0] == "no" && labels[1] == "yes")
}

// ID implements [Dialog].
func (*AskUser) ID() string {
	return AskUserID
}

// HandleMsg implements [Dialog].
func (d *AskUser) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return d.handleKeyPress(msg)
	case tea.PasteMsg:
		return d.handlePaste(msg)
	}
	return nil
}

func (d *AskUser) handleKeyPress(msg tea.KeyPressMsg) Action {
	qs := &d.questions[d.active]

	// If text input is active, route most keys to it.
	if qs.otherActive || qs.qType == questionTypeFreeText {
		return d.handleTextInputKey(msg, qs)
	}

	switch {
	case key.Matches(msg, d.keyMap.Close):
		return d.cancel()
	case key.Matches(msg, d.keyMap.Up):
		d.moveCursor(-1)
	case key.Matches(msg, d.keyMap.Down):
		d.moveCursor(1)
	case key.Matches(msg, d.keyMap.Left):
		d.switchQuestion(-1)
	case key.Matches(msg, d.keyMap.Right):
		d.switchQuestion(1)
	case key.Matches(msg, d.keyMap.Toggle):
		if qs.qType == questionTypeMultiSelect {
			d.toggleOption()
		}
	case key.Matches(msg, d.keyMap.Select):
		return d.handleSelect()
	}
	return nil
}

func (d *AskUser) handleTextInputKey(msg tea.KeyPressMsg, qs *questionState) Action {
	switch {
	case key.Matches(msg, d.keyMap.Close):
		return d.cancel()
	case key.Matches(msg, d.keyMap.Select):
		return d.advanceOrSubmit()
	default:
		var cmd tea.Cmd
		qs.textInput, cmd = qs.textInput.Update(msg)
		return ActionCmd{Cmd: cmd}
	}
}

func (d *AskUser) handlePaste(msg tea.PasteMsg) Action {
	qs := &d.questions[d.active]
	if qs.otherActive || qs.qType == questionTypeFreeText {
		var cmd tea.Cmd
		qs.textInput, cmd = qs.textInput.Update(msg)
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *AskUser) cancel() Action {
	return ActionAskUserResponse{
		RequestID:  d.request.ID,
		ToolCallID: d.request.ToolCallID,
		Response:   askuser.Response{Canceled: true},
	}
}

func (d *AskUser) moveCursor(delta int) {
	qs := &d.questions[d.active]
	q := d.request.Questions[d.active]

	optCount := len(q.Options)
	if qs.qType == questionTypeSingleSelect || qs.qType == questionTypeMultiSelect {
		optCount++ // "Other..." option
	}

	if optCount == 0 {
		return
	}

	qs.cursor = ((qs.cursor + delta) + optCount) % optCount

	// If moving away from "Other...", deactivate text input.
	otherIdx := len(q.Options)
	if qs.cursor != otherIdx && qs.otherActive {
		qs.otherActive = false
		qs.textInput.Blur()
	}
}

func (d *AskUser) switchQuestion(delta int) {
	n := len(d.questions)
	if n <= 1 {
		return
	}
	// Blur current text input if active.
	qs := &d.questions[d.active]
	if qs.otherActive || qs.qType == questionTypeFreeText {
		qs.textInput.Blur()
		qs.otherActive = false
	}

	d.active = ((d.active + delta) + n) % n

	// Focus new question's text input if free text.
	newQs := &d.questions[d.active]
	if newQs.qType == questionTypeFreeText {
		newQs.textInput.Focus()
	}
}

func (d *AskUser) toggleOption() {
	qs := &d.questions[d.active]
	q := d.request.Questions[d.active]

	otherIdx := len(q.Options)
	if qs.cursor == otherIdx {
		// Toggle "Other..." — activate text input.
		if qs.selected[qs.cursor] {
			delete(qs.selected, qs.cursor)
			qs.otherActive = false
			qs.textInput.Blur()
		} else {
			qs.selected[qs.cursor] = true
			qs.otherActive = true
			qs.textInput.Focus()
		}
		return
	}

	if qs.selected[qs.cursor] {
		delete(qs.selected, qs.cursor)
	} else {
		qs.selected[qs.cursor] = true
	}
}

func (d *AskUser) handleSelect() Action {
	qs := &d.questions[d.active]
	q := d.request.Questions[d.active]

	switch qs.qType {
	case questionTypeSingleSelect:
		otherIdx := len(q.Options)
		if qs.cursor == otherIdx {
			if !qs.otherActive {
				// Activate "Other..." text input.
				qs.otherActive = true
				qs.textInput.Focus()
				return nil
			}
			// "Other..." is active — submit its text.
			return d.advanceOrSubmit()
		}
		// Select this option and advance.
		return d.advanceOrSubmit()

	case questionTypeMultiSelect:
		// In multi-select, enter submits the current selections.
		return d.advanceOrSubmit()

	case questionTypeConfirm:
		// Select the currently highlighted confirm option.
		qs.confirmed = &qs.cursor
		return d.advanceOrSubmit()

	case questionTypeFreeText:
		return d.advanceOrSubmit()
	}

	return nil
}

func (d *AskUser) advanceOrSubmit() Action {
	if d.active < len(d.questions)-1 {
		d.switchQuestion(1)
		return nil
	}
	// Last question — submit all answers.
	return d.submit()
}

func (d *AskUser) submit() Action {
	answers := make([]askuser.Answer, len(d.request.Questions))
	for i, q := range d.request.Questions {
		qs := &d.questions[i]
		answers[i] = askuser.Answer{
			ID:       q.ID,
			Question: q.Question,
			Values:   d.collectValues(i, qs, q),
		}
	}

	return ActionAskUserResponse{
		RequestID:  d.request.ID,
		ToolCallID: d.request.ToolCallID,
		Response: askuser.Response{
			Answers: answers,
		},
	}
}

func (d *AskUser) collectValues(_ int, qs *questionState, q askuser.Question) []string {
	switch qs.qType {
	case questionTypeFreeText:
		return collectTextValue(qs)
	case questionTypeSingleSelect:
		return collectSingleSelectValue(qs, q)
	case questionTypeMultiSelect:
		return collectMultiSelectValues(qs, q)
	case questionTypeConfirm:
		return collectConfirmValue(qs, q)
	}
	return nil
}

func collectTextValue(qs *questionState) []string {
	text := strings.TrimSpace(qs.textInput.Value())
	if text == "" {
		return nil
	}
	return []string{text}
}

func collectSingleSelectValue(qs *questionState, q askuser.Question) []string {
	otherIdx := len(q.Options)
	if qs.otherActive || qs.cursor == otherIdx {
		return collectTextValue(qs)
	}
	if qs.cursor < len(q.Options) {
		return []string{q.Options[qs.cursor].Label}
	}
	return nil
}

func collectMultiSelectValues(qs *questionState, q askuser.Question) []string {
	var values []string
	for j, opt := range q.Options {
		if qs.selected[j] {
			values = append(values, opt.Label)
		}
	}
	if qs.selected[len(q.Options)] {
		text := strings.TrimSpace(qs.textInput.Value())
		if text != "" {
			values = append(values, text)
		}
	}
	return values
}

func collectConfirmValue(qs *questionState, q askuser.Question) []string {
	if qs.confirmed != nil && *qs.confirmed < len(q.Options) {
		return []string{q.Options[*qs.confirmed].Label}
	}
	return nil
}

// Draw implements [Dialog].
func (d *AskUser) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	dialogStyle := t.Dialog.View.Padding(0, 1)

	// Calculate width.
	width := min(int(float64(area.Dx())*0.6), askUserMaxWidth)
	dialogStyle = dialogStyle.Width(width)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize()

	// Header.
	header := d.renderHeader(contentWidth)

	// Question chips.
	chips := d.renderChips(contentWidth)

	// Current question content.
	content := d.renderQuestion(contentWidth)

	// Help.
	helpView := d.help.View(d)

	parts := []string{header, "", chips, "", content, "", helpView}
	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	DrawCenterCursor(scr, area, dialogStyle.Render(innerContent), nil)
	return nil
}

func (d *AskUser) renderHeader(width int) string {
	t := d.com.Styles
	title := common.DialogTitle(t, "OPERATOR INPUT REQUIRED", width-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	return t.Dialog.Title.Render(title)
}

func (d *AskUser) renderChips(width int) string {
	t := d.com.Styles
	chips := make([]string, 0, len(d.request.Questions))
	for i, q := range d.request.Questions {
		label := q.Header
		if label == "" {
			label = q.ID
		}
		label = strings.ToUpper(label)

		var chipStyle lipgloss.Style
		if i == d.active {
			chipStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(t.BgBase).
				Background(t.Primary).
				Padding(0, 1)
		} else {
			chipStyle = lipgloss.NewStyle().
				Foreground(t.Muted.GetForeground()).
				Background(t.BgSubtle).
				Padding(0, 1)
		}
		chips = append(chips, chipStyle.Render(label))
	}

	chipRow := strings.Join(chips, "  ")
	if lipgloss.Width(chipRow) > width {
		chipRow = strings.Join(chips, " ")
	}
	return chipRow
}

func (d *AskUser) renderQuestion(width int) string {
	q := d.request.Questions[d.active]
	qs := &d.questions[d.active]

	// Question text.
	questionStyle := lipgloss.NewStyle().Bold(true).Width(width)
	questionText := questionStyle.Render(q.Question)

	var optionsView string
	switch qs.qType {
	case questionTypeSingleSelect:
		optionsView = d.renderSingleSelect(q, qs, width)
	case questionTypeMultiSelect:
		optionsView = d.renderMultiSelect(q, qs, width)
	case questionTypeFreeText:
		qs.textInput.SetWidth(width - 4)
		optionsView = qs.textInput.View()
	case questionTypeConfirm:
		optionsView = d.renderConfirm(q, qs)
	}

	parts := []string{questionText, "", optionsView}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (d *AskUser) renderSingleSelect(q askuser.Question, qs *questionState, width int) string {
	t := d.com.Styles
	var lines []string

	for i, opt := range q.Options {
		indicator := "○"
		if i == qs.cursor && !qs.otherActive {
			indicator = "●"
		}

		var labelStyle, descStyle lipgloss.Style
		if i == qs.cursor && !qs.otherActive {
			labelStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
			descStyle = lipgloss.NewStyle().Foreground(t.Muted.GetForeground())
		} else {
			labelStyle = lipgloss.NewStyle()
			descStyle = lipgloss.NewStyle().Foreground(t.Muted.GetForeground())
		}

		line := indicator + " " + labelStyle.Render(opt.Label)
		if opt.Description != "" {
			line += "\n  " + descStyle.Width(width-4).Render(opt.Description)
		}
		lines = append(lines, line)
	}

	// "Other..." option.
	otherIdx := len(q.Options)
	otherIndicator := "○"
	if qs.cursor == otherIdx || qs.otherActive {
		otherIndicator = "●"
	}
	otherStyle := lipgloss.NewStyle()
	if qs.cursor == otherIdx || qs.otherActive {
		otherStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	}
	lines = append(lines, otherIndicator+" "+otherStyle.Render(otherLabel))

	if qs.otherActive {
		qs.textInput.SetWidth(width - 4)
		lines = append(lines, "  "+qs.textInput.View())
	}

	return strings.Join(lines, "\n")
}

func (d *AskUser) renderMultiSelect(q askuser.Question, qs *questionState, width int) string {
	t := d.com.Styles
	var lines []string

	for i, opt := range q.Options {
		checkbox := "☐"
		if qs.selected[i] {
			checkbox = "☑"
		}

		var labelStyle, descStyle lipgloss.Style
		if i == qs.cursor {
			labelStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
			descStyle = lipgloss.NewStyle().Foreground(t.Muted.GetForeground())
		} else {
			labelStyle = lipgloss.NewStyle()
			descStyle = lipgloss.NewStyle().Foreground(t.Muted.GetForeground())
		}

		line := checkbox + " " + labelStyle.Render(opt.Label)
		if opt.Description != "" {
			line += "\n  " + descStyle.Width(width-4).Render(opt.Description)
		}
		lines = append(lines, line)
	}

	// "Other..." option.
	otherIdx := len(q.Options)
	otherCheckbox := "☐"
	if qs.selected[otherIdx] {
		otherCheckbox = "☑"
	}
	otherStyle := lipgloss.NewStyle()
	if qs.cursor == otherIdx {
		otherStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	}
	lines = append(lines, otherCheckbox+" "+otherStyle.Render(otherLabel))

	if qs.otherActive {
		qs.textInput.SetWidth(width - 4)
		lines = append(lines, "  "+qs.textInput.View())
	}

	return strings.Join(lines, "\n")
}

func (d *AskUser) renderConfirm(q askuser.Question, qs *questionState) string {
	t := d.com.Styles
	buttons := make([]common.ButtonOpts, len(q.Options))
	for i, opt := range q.Options {
		buttons[i] = common.ButtonOpts{
			Text:     opt.Label,
			Selected: i == qs.cursor,
		}
	}
	return common.ButtonGroup(t, buttons, "  ")
}

// ShortHelp implements [help.KeyMap].
func (d *AskUser) ShortHelp() []key.Binding {
	bindings := []key.Binding{d.keyMap.Select, d.keyMap.Close}
	if len(d.questions) > 1 {
		bindings = append([]key.Binding{d.keyMap.NavQ}, bindings...)
	}
	qs := &d.questions[d.active]
	if qs.qType == questionTypeSingleSelect || qs.qType == questionTypeMultiSelect {
		bindings = append([]key.Binding{d.keyMap.NavOpts}, bindings...)
	}
	if qs.qType == questionTypeMultiSelect {
		bindings = append(bindings, d.keyMap.Toggle)
	}
	return bindings
}

// FullHelp implements [help.KeyMap].
func (d *AskUser) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
