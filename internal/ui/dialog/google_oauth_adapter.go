package dialog

import (
	tea "charm.land/bubbletea/v2"
	"github.com/dmora/crucible/internal/oauth"
)

// googleOAuthAdapter wraps oauth.GoogleProvider to satisfy the unexported
// OAuthProvider interface used by the OAuth dialog.
type googleOAuthAdapter struct {
	*oauth.GoogleProvider
}

func (a googleOAuthAdapter) name() string { return a.Name() }

func (a googleOAuthAdapter) initiateAuth() tea.Msg {
	msg := a.InitiateAuth()
	switch m := msg.(type) {
	case oauth.OAuthInitMsg:
		return ActionInitiateOAuth{
			AuthURL: m.AuthURL,
			Port:    m.Port,
		}
	case oauth.OAuthErrorMsg:
		return ActionOAuthErrored{Error: m.Err}
	default:
		return msg
	}
}

func (a googleOAuthAdapter) awaitCallback(expiresIn int) tea.Cmd {
	innerCmd := a.AwaitCallback(expiresIn)
	return func() tea.Msg {
		msg := innerCmd()
		switch m := msg.(type) {
		case oauth.OAuthCompleteMsg:
			return ActionCompleteOAuth{Token: m.Token}
		case oauth.OAuthErrorMsg:
			return ActionOAuthErrored{Error: m.Err}
		default:
			return msg
		}
	}
}

func (a googleOAuthAdapter) cancel() tea.Msg { return a.Cancel() }

// newGoogleOAuthAdapter creates an OAuthProvider backed by oauth.GoogleProvider.
func newGoogleOAuthAdapter() OAuthProvider {
	return googleOAuthAdapter{oauth.NewGoogleProvider()}
}
