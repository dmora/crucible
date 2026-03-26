package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/oauth"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/util"
	"github.com/pkg/browser"
)

// OAuthProvider is the interface for OAuth providers used by the dialog.
type OAuthProvider interface {
	name() string
	initiateAuth() tea.Msg
	awaitCallback(expiresIn int) tea.Cmd
	cancel() tea.Msg
}

// OAuthState represents the current state of the OAuth flow.
type OAuthState int

const (
	OAuthStateInitializing OAuthState = iota
	OAuthStateDisplay
	OAuthStateSuccess
	OAuthStateError
)

// OAuthID is the identifier for the OAuth dialog.
const OAuthID = "oauth"

// OAuth handles the OAuth flow authentication.
type OAuth struct {
	com          *common.Common
	isOnboarding bool

	provider      config.ProviderMetadata
	model         config.SelectedModel
	modelType     config.SelectedModelType
	oAuthProvider OAuthProvider

	State OAuthState

	spinner spinner.Model
	help    help.Model
	keyMap  struct {
		Submit key.Binding
		Close  key.Binding
	}

	width     int
	authURL   string // for fallback "open manually" link
	expiresIn int
	token     *oauth.Token
}

var _ Dialog = (*OAuth)(nil)

// newOAuth creates a new OAuth dialog component.
func newOAuth(
	com *common.Common,
	isOnboarding bool,
	provider config.ProviderMetadata,
	model config.SelectedModel,
	modelType config.SelectedModelType,
	oAuthProvider OAuthProvider,
) (*OAuth, tea.Cmd) {
	t := com.Styles

	m := OAuth{}
	m.com = com
	m.isOnboarding = isOnboarding
	m.provider = provider
	m.model = model
	m.modelType = modelType
	m.oAuthProvider = oAuthProvider
	m.width = 60
	m.State = OAuthStateInitializing

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(t.Base.Foreground(t.GreenLight)),
	)

	m.help = help.New()
	m.help.Styles = t.DialogHelpStyles()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "finish"),
	)
	m.keyMap.Close = CloseKey

	return &m, tea.Batch(m.spinner.Tick, m.oAuthProvider.initiateAuth)
}

// ID implements Dialog.
func (m *OAuth) ID() string {
	return OAuthID
}

// HandleMsg handles messages and state transitions.
func (m *OAuth) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		switch m.State {
		case OAuthStateInitializing, OAuthStateDisplay:
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				return ActionCmd{cmd}
			}
		}

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)

	case ActionInitiateOAuth:
		m.authURL = msg.AuthURL
		m.expiresIn = 300 // 5 minute timeout for callback
		m.State = OAuthStateDisplay
		// Auto-open browser and start waiting for callback
		return ActionCmd{tea.Batch(
			func() tea.Msg {
				if err := browser.OpenURL(msg.AuthURL); err != nil {
					return ActionOAuthErrored{Error: fmt.Errorf("open browser: %w", err)}
				}
				return nil
			},
			m.oAuthProvider.awaitCallback(300),
		)}

	case ActionCompleteOAuth:
		m.State = OAuthStateSuccess
		m.token = msg.Token
		return ActionCmd{m.oAuthProvider.cancel}

	case ActionOAuthErrored:
		m.State = OAuthStateError
		cmd := tea.Batch(m.oAuthProvider.cancel, util.ReportError(msg.Error))
		return ActionCmd{cmd}
	}
	return nil
}

func (m *OAuth) handleKeyPress(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, m.keyMap.Submit):
		if m.State == OAuthStateSuccess {
			return m.saveKeyAndContinue()
		}

	case key.Matches(msg, m.keyMap.Close):
		if m.State == OAuthStateSuccess {
			return m.saveKeyAndContinue()
		}
		// Synchronously tear down listener + server, then close dialog.
		// cancel() is non-blocking (mutex-guarded Close, not Shutdown).
		m.oAuthProvider.cancel()
		return ActionClose{}
	}
	return nil
}

// Draw renders the OAuth dialog.
func (m *OAuth) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	var (
		t           = m.com.Styles
		dialogStyle = t.Dialog.View.Width(m.width)
	)
	if m.isOnboarding {
		view := m.dialogContent()
		DrawOnboarding(scr, area, view)
	} else {
		view := dialogStyle.Render(m.dialogContent())
		DrawCenter(scr, area, view)
	}
	return nil
}

func (m *OAuth) dialogContent() string {
	var (
		t         = m.com.Styles
		helpStyle = t.Dialog.HelpView
	)

	switch m.State {
	case OAuthStateInitializing:
		return m.innerDialogContent()

	default:
		elements := []string{
			m.headerContent(),
			m.innerDialogContent(),
			helpStyle.Render(m.help.View(m)),
		}
		return strings.Join(elements, "\n")
	}
}

func (m *OAuth) headerContent() string {
	var (
		t            = m.com.Styles
		titleStyle   = t.Dialog.Title
		textStyle    = t.Dialog.PrimaryText
		dialogStyle  = t.Dialog.View.Width(m.width)
		headerOffset = titleStyle.GetHorizontalFrameSize() + dialogStyle.GetHorizontalFrameSize()
		dialogTitle  = fmt.Sprintf("Authenticate with %s", m.oAuthProvider.name())
	)
	if m.isOnboarding {
		return textStyle.Render(dialogTitle)
	}
	return common.DialogTitle(t, titleStyle.Render(dialogTitle), m.width-headerOffset, t.Primary, t.Secondary)
}

func (m *OAuth) innerDialogContent() string {
	var (
		t          = m.com.Styles
		whiteStyle = lipgloss.NewStyle().Foreground(t.White)
		greenStyle = lipgloss.NewStyle().Foreground(t.GreenLight)
		linkStyle  = lipgloss.NewStyle().Foreground(t.GreenDark).Underline(true)
		errorStyle = lipgloss.NewStyle().Foreground(t.Error)
		mutedStyle = lipgloss.NewStyle().Foreground(t.FgMuted)
	)

	switch m.State {
	case OAuthStateInitializing:
		return lipgloss.NewStyle().
			Margin(1, 1).
			Width(m.width - 2).
			Align(lipgloss.Center).
			Render(
				greenStyle.Render(m.spinner.View()) +
					mutedStyle.Render("Initializing..."),
			)

	case OAuthStateDisplay:
		instructions := lipgloss.NewStyle().
			Margin(0, 1).
			Width(m.width - 2).
			Render(
				whiteStyle.Render("Sign in with your Google account in the browser."),
			)

		link := linkStyle.Hyperlink(m.authURL, "id=oauth-verify").Render(m.authURL)
		url := mutedStyle.
			Margin(0, 1).
			Width(m.width - 2).
			Render("Browser not opening? Open this link:\n" + link)

		waiting := lipgloss.NewStyle().
			Margin(0, 1).
			Width(m.width - 2).
			Render(
				greenStyle.Render(m.spinner.View()) + mutedStyle.Render("Waiting for sign-in..."),
			)

		return lipgloss.JoinVertical(
			lipgloss.Left,
			"",
			instructions,
			"",
			url,
			"",
			waiting,
			"",
		)

	case OAuthStateSuccess:
		return greenStyle.
			Margin(1).
			Width(m.width - 2).
			Render("Authentication successful!")

	case OAuthStateError:
		return lipgloss.NewStyle().
			Margin(1).
			Width(m.width - 2).
			Render(errorStyle.Render("Authentication failed."))

	default:
		return ""
	}
}

// FullHelp returns the full help view.
func (m *OAuth) FullHelp() [][]key.Binding {
	return [][]key.Binding{m.ShortHelp()}
}

// ShortHelp returns the short help view.
func (m *OAuth) ShortHelp() []key.Binding {
	switch m.State {
	case OAuthStateError:
		return []key.Binding{m.keyMap.Close}

	case OAuthStateSuccess:
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("enter", "ctrl+y", "esc"),
				key.WithHelp("enter", "finish"),
			),
		}

	default:
		return []key.Binding{m.keyMap.Close}
	}
}

func (m *OAuth) saveKeyAndContinue() Action {
	return ActionCredentialReady{
		ProviderID: m.provider.ID,
		Credential: config.Credential{
			Kind:       config.CredentialOAuth,
			OAuthToken: m.token,
		},
		SelectModel: &ActionSelectModel{
			Provider:  m.provider,
			Model:     m.model,
			ModelType: m.modelType,
		},
	}
}

// NewOAuthForAuth creates an OAuth dialog for the auth-switch flow.
func NewOAuthForAuth(
	com *common.Common,
	provider config.ProviderMetadata,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, false, provider, model, modelType, newGoogleOAuthAdapter())
}
