package dialog

import (
	"fmt"
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

const (
	authDialogMaxWidth  = 60
	authDialogMaxHeight = 12
)

// Auth is a dialog for selecting the authentication method at runtime.
type Auth struct {
	com        *common.Common
	providerID string
	provider   config.ProviderMetadata
	modelCfg   config.SelectedModel
	modelType  config.SelectedModelType
	help       help.Model
	list       *list.FilterableList

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Auth)(nil)

// NewAuth creates a new Auth dialog populated with detected auth methods.
func NewAuth(
	com *common.Common,
	providerID string,
	provider config.ProviderMetadata,
	modelCfg config.SelectedModel,
	modelType config.SelectedModelType,
) (*Auth, tea.Cmd) {
	a := &Auth{
		com:        com,
		providerID: providerID,
		provider:   provider,
		modelCfg:   modelCfg,
		modelType:  modelType,
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	a.help = h

	a.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	)
	a.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n", "j"),
	)
	a.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p", "k"),
	)
	a.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	a.keyMap.Close = CloseKey

	a.buildItems()
	return a, nil
}

func (a *Auth) buildItems() {
	currentAuth := a.currentAuth()
	providerCfg, _ := a.com.Config().Providers.Get(a.providerID)
	methods := config.DetectAuthMethods(currentAuth, providerCfg, currentAuth.Backend)

	items := make([]list.FilterableItem, 0, len(methods))
	for _, m := range methods {
		items = append(items, newAuthMethodItem(a.com.Styles, m))
	}
	a.list = list.NewFilterableList(items...)
	a.list.Focus()
}

func (a *Auth) currentAuth() config.AuthInfo {
	app := a.com.App
	if app != nil && app.AgentCoordinator != nil {
		return app.AgentCoordinator.Model().Auth
	}
	return config.AuthInfo{}
}

func (a *Auth) dialogTitle() string {
	currentAuth := a.currentAuth()
	var backendLabel string
	switch currentAuth.Backend {
	case config.GeminiBackendVertex:
		backendLabel = "VERTEX AI"
	case config.GeminiBackendAPI:
		backendLabel = "GEMINI API"
	default:
		backendLabel = "UNKNOWN"
	}
	return strings.ToUpper("PROVIDER: "+a.providerID) + " · " + backendLabel
}

// ID returns the dialog identifier.
func (a *Auth) ID() string { return AuthID }

// ShortHelp returns the short help key bindings.
func (a *Auth) ShortHelp() []key.Binding {
	return []key.Binding{a.keyMap.UpDown, a.keyMap.Select, a.keyMap.Close}
}

// FullHelp returns the full help key bindings.
func (a *Auth) FullHelp() [][]key.Binding {
	return [][]key.Binding{{a.keyMap.Select, a.keyMap.Next, a.keyMap.Previous, a.keyMap.Close}}
}

// HandleMsg processes input for the Auth dialog.
func (a *Auth) HandleMsg(msg tea.Msg) Action {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, a.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, a.keyMap.Select):
			return a.handleSelection()
		case key.Matches(msg, a.keyMap.Next):
			a.list.SelectNext()
			return nil
		case key.Matches(msg, a.keyMap.Previous):
			a.list.SelectPrev()
			return nil
		}
	}
	return nil
}

func (a *Auth) handleSelection() Action {
	item, ok := a.list.SelectedItem().(*AuthMethodItem)
	if !ok {
		return nil
	}

	if item.avail.Status == config.AuthStatusUnavailable {
		return nil
	}
	if item.avail.Status == config.AuthStatusCurrent {
		return ActionClose{}
	}

	switch item.avail.Method {
	case config.AuthMethodAPIKey:
		dlg, cmd := NewAPIKeyInput(a.com, false, a.provider, a.modelCfg, a.modelType)
		return ActionOpenSubDialog{Dialog: dlg, Cmd: cmd}

	case config.AuthMethodUserADC:
		return ActionCredentialReady{
			ProviderID: a.providerID,
			Credential: config.Credential{Kind: config.CredentialADC},
		}

	case config.AuthMethodServiceAccount:
		provID := a.providerID
		dlg, cmd := NewFilePicker(a.com,
			WithAllowedTypes([]string{".json"}),
			WithOnSelect(func(path string) Action {
				method, _ := config.ReadCredentialsFile(path)
				if method != config.AuthMethodServiceAccount {
					return ActionCmd{Cmd: util.ReportError(
						fmt.Errorf("selected file is not a valid service account key"),
					)}
				}
				return ActionCredentialReady{
					ProviderID: provID,
					Credential: config.Credential{
						Kind:            config.CredentialServiceAccount,
						CredentialsFile: path,
					},
				}
			}),
		)
		return ActionOpenSubDialog{Dialog: dlg, Cmd: cmd}

	case config.AuthMethodOAuth:
		dlg, cmd := NewOAuthForAuth(a.com, a.provider, a.modelCfg, a.modelType)
		return ActionOpenSubDialog{Dialog: dlg, Cmd: cmd}

	default:
		return nil
	}
}

// Draw renders the Auth dialog.
func (a *Auth) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	width := min(authDialogMaxWidth, area.Dx())
	height := min(authDialogMaxHeight, area.Dy())

	t := a.com.Styles
	rc := NewRenderContext(t, width)
	rc.Gap = 1
	rc.Title = a.dialogTitle()
	rc.Help = a.help.View(a)

	a.list.SetSize(width-t.Dialog.View.GetHorizontalFrameSize(), height)
	rc.AddPart(a.list.Render())

	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}
