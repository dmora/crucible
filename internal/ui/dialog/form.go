package dialog

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dmora/crucible/internal/ui/styles"
)

// FormFieldKind identifies the input type for a form field.
type FormFieldKind int

const (
	FormFieldText     FormFieldKind = iota // single-line textinput
	FormFieldTextArea                      // multi-line textarea
	FormFieldCycle                         // cycling selector (Enter to advance)
	FormFieldToggle                        // bool toggle (Space to flip)
)

// FormField describes a single editable field in a Form.
type FormField struct {
	Label     string
	Key       string // config field key
	Kind      FormFieldKind
	Value     string   // current value for text/cycle
	Options   []string // for Cycle: ordered options
	BoolValue bool     // for Toggle
	ReadOnly  bool     // display-only
}

// FormResult indicates how the user exited the form.
type FormResult int

const (
	FormNone   FormResult = iota
	FormSave              // Ctrl+S
	FormCancel            // Escape
)

const (
	formFieldHeight    = 3 // label + input + spacing per text/cycle/toggle field
	formTextAreaHeight = 5 // label + 2-line textarea + spacing
	formMaxViewportH   = 24
)

// Form is a multi-field input component with text, textarea, cycle, and toggle fields.
type Form struct {
	fields     []FormField
	focused    int
	editing    bool              // true when text input/textarea is actively receiving keys
	textInputs []textinput.Model // indexed by field position (nil entry for non-text)
	textAreas  []textarea.Model  // indexed by field position (nil entry for non-textarea)
	viewport   viewport.Model
	styles     *styles.Styles
}

// NewForm creates a Form for the given fields.
func NewForm(sty *styles.Styles, fields []FormField) *Form {
	f := &Form{
		fields:     fields,
		textInputs: make([]textinput.Model, len(fields)),
		textAreas:  make([]textarea.Model, len(fields)),
		styles:     sty,
	}

	for i, fd := range fields {
		switch fd.Kind {
		case FormFieldText:
			ti := textinput.New()
			ti.SetVirtualCursor(false)
			ti.SetStyles(sty.TextInput)
			ti.Prompt = "> "
			ti.SetValue(fd.Value)
			ti.Blur()
			f.textInputs[i] = ti
		case FormFieldTextArea:
			ta := textarea.New()
			ta.SetHeight(2)
			ta.SetValue(fd.Value)
			ta.Blur()
			f.textAreas[i] = ta
		}
	}
	return f
}

// Values returns the current field values indexed by Key.
func (f *Form) Values() map[string]FormField {
	result := make(map[string]FormField, len(f.fields))
	for i, fd := range f.fields {
		switch fd.Kind {
		case FormFieldText:
			fd.Value = f.textInputs[i].Value()
		case FormFieldTextArea:
			fd.Value = f.textAreas[i].Value()
		}
		result[fd.Key] = fd
	}
	return result
}

// fieldHeight returns the rendered height for a field at index i.
func (f *Form) fieldHeight(i int) int {
	if f.fields[i].Kind == FormFieldTextArea {
		return formTextAreaHeight
	}
	return formFieldHeight
}

// fieldOffset returns the y-offset for the start of field i.
func (f *Form) fieldOffset(i int) int {
	offset := 0
	for j := 0; j < i; j++ {
		offset += f.fieldHeight(j)
	}
	return offset
}

// HandleMsg processes input for the form. Returns FormSave, FormCancel, or FormNone.
func (f *Form) HandleMsg(msg tea.Msg) FormResult {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		return f.handleKey(msg)
	}
	return FormNone
}

func (f *Form) handleKey(msg tea.KeyPressMsg) FormResult {
	fd := &f.fields[f.focused]

	// If editing a text/textarea field, route keys there first.
	if f.editing {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			f.deactivateField()
			return FormNone
		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+s"))):
			f.deactivateField()
			return FormSave
		default:
			f.updateActiveField(msg)
			return FormNone
		}
	}

	// Not editing — handle navigation.
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		return FormCancel
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+s"))):
		return FormSave
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down"))):
		f.focusField(f.focused + 1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
		f.focusField(f.focused - 1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		f.activateField(fd)
	case key.Matches(msg, key.NewBinding(key.WithKeys(" "))):
		if fd.Kind == FormFieldToggle && !fd.ReadOnly {
			fd.BoolValue = !fd.BoolValue
		}
	}
	return FormNone
}

func (f *Form) activateField(fd *FormField) {
	if fd.ReadOnly {
		return
	}
	switch fd.Kind {
	case FormFieldText:
		f.editing = true
		f.textInputs[f.focused].Focus()
	case FormFieldTextArea:
		f.editing = true
		f.textAreas[f.focused].Focus()
	case FormFieldCycle:
		if len(fd.Options) > 0 {
			idx := 0
			for i, opt := range fd.Options {
				if opt == fd.Value {
					idx = i
					break
				}
			}
			idx = (idx + 1) % len(fd.Options)
			fd.Value = fd.Options[idx]
		}
	}
}

func (f *Form) deactivateField() {
	fd := &f.fields[f.focused]
	switch fd.Kind {
	case FormFieldText:
		f.textInputs[f.focused].Blur()
	case FormFieldTextArea:
		f.textAreas[f.focused].Blur()
	}
	f.editing = false
}

func (f *Form) updateActiveField(msg tea.KeyPressMsg) {
	fd := &f.fields[f.focused]
	switch fd.Kind {
	case FormFieldText:
		f.textInputs[f.focused], _ = f.textInputs[f.focused].Update(msg)
	case FormFieldTextArea:
		f.textAreas[f.focused], _ = f.textAreas[f.focused].Update(msg)
	}
}

func (f *Form) focusField(idx int) {
	n := len(f.fields)
	f.focused = ((idx % n) + n) % n
	f.ensureFieldVisible(f.focused)
}

func (f *Form) ensureFieldVisible(idx int) {
	start := f.fieldOffset(idx)
	end := start + f.fieldHeight(idx) - 1
	top := f.viewport.YOffset()
	h := f.viewport.Height()

	if start < top {
		f.viewport.SetYOffset(start)
	} else if end > top+h-1 {
		f.viewport.SetYOffset(end - h + 1)
	}
}

// Render returns the form content as a string for embedding in a dialog.
func (f *Form) Render(width int) string {
	fields := make([]string, len(f.fields))
	for i := range f.fields {
		fields[i] = f.renderField(i, width)
	}

	rendered := lipgloss.JoinVertical(lipgloss.Left, fields...)

	totalH := lipgloss.Height(rendered)
	vpH := min(totalH, formMaxViewportH)
	f.viewport.SetWidth(width)
	f.viewport.SetHeight(vpH)
	f.viewport.SetContent(rendered)

	return f.viewport.View()
}

func (f *Form) renderField(i, width int) string {
	fd := f.fields[i]
	isFocused := i == f.focused
	sty := f.styles

	labelStyle := sty.Muted
	if isFocused && !fd.ReadOnly {
		labelStyle = sty.Dialog.PrimaryText
	}
	label := labelStyle.Render(fd.Label)
	valueLine := f.renderFieldValue(i, fd, isFocused, width)

	return lipgloss.JoinVertical(lipgloss.Left, label, valueLine, "")
}

func (f *Form) renderFieldValue(i int, fd FormField, isFocused bool, width int) string {
	sty := f.styles
	switch fd.Kind {
	case FormFieldText:
		return f.renderTextValue(i, isFocused, width)
	case FormFieldTextArea:
		return f.renderTextAreaValue(i, isFocused)
	case FormFieldCycle:
		v := fd.Value
		if v == "" {
			v = "(default)"
		}
		if isFocused {
			return sty.Dialog.PrimaryText.Render("◂ " + v + " ▸")
		}
		return sty.Subtle.Render(v)
	case FormFieldToggle:
		check := "[ ]"
		if fd.BoolValue {
			check = "[x]"
		}
		if isFocused {
			return sty.Dialog.PrimaryText.Render(check)
		}
		return sty.Subtle.Render(check)
	default:
		return ""
	}
}

func (f *Form) renderTextValue(i int, isFocused bool, width int) string {
	if f.editing && isFocused {
		f.textInputs[i].SetWidth(min(width-4, 60))
		return f.textInputs[i].View()
	}
	v := f.textInputs[i].Value()
	if v == "" {
		v = "(empty)"
	}
	return f.styles.Subtle.Render(v)
}

func (f *Form) renderTextAreaValue(i int, isFocused bool) string {
	if f.editing && isFocused {
		return f.textAreas[i].View()
	}
	v := f.textAreas[i].Value()
	if v == "" {
		v = "(empty)"
	}
	return f.styles.Subtle.Render(v)
}

// Cursor returns the cursor for the currently active text input.
func (f *Form) Cursor() *tea.Cursor {
	if !f.editing {
		return nil
	}
	fd := f.fields[f.focused]
	switch fd.Kind {
	case FormFieldText:
		cur := InputCursor(f.styles, f.textInputs[f.focused].Cursor())
		if cur != nil {
			// Adjust for field position within viewport: +1 for the label line
			// above the input, offset by field start, minus viewport scroll.
			cur.Y += f.fieldOffset(f.focused) + 1 - f.viewport.YOffset()
		}
		return cur
	}
	return nil
}

// FormHelpBindings returns the form-level help key bindings.
func FormHelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab/↑↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit/cycle")),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}
