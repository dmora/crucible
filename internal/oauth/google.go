package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/oauth2"
)

const (
	googleAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL   = "https://oauth2.googleapis.com/token"                    //nolint:gosec // Token endpoint URL, not a credential
	googleOAuthScope = "https://www.googleapis.com/auth/generative-language " + //nolint:gosec // OAuth scope URL, not a credential
		"https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email"
	// Default OAuth client credentials for installed/native apps (localhost redirect).
	// Override with CRUCIBLE_OAUTH_CLIENT_ID and CRUCIBLE_OAUTH_CLIENT_SECRET env vars.
	defaultGoogleClientID     = ""
	defaultGoogleClientSecret = ""
)

// googleClientID returns the OAuth client ID from env or default.
func googleClientID() string {
	if v := os.Getenv("CRUCIBLE_OAUTH_CLIENT_ID"); v != "" {
		return v
	}
	return defaultGoogleClientID
}

// googleClientSecret returns the OAuth client secret from env or default.
func googleClientSecret() string {
	if v := os.Getenv("CRUCIBLE_OAUTH_CLIENT_SECRET"); v != "" {
		return v
	}
	return defaultGoogleClientSecret
}

// OAuthConfig returns the oauth2.Config for the Google OAuth flow.
// Used by coordinator.go to build a refreshable TokenSource.
func OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     googleClientID(),
		ClientSecret: googleClientSecret(),
		Endpoint: oauth2.Endpoint{
			AuthURL:  googleAuthURL,
			TokenURL: googleTokenURL,
		},
		Scopes: []string{
			"https://www.googleapis.com/auth/generative-language",
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
		},
	}
}

// GoogleProvider implements the OAuth2 authorization code flow with localhost
// redirect for Google accounts.
type GoogleProvider struct {
	mu       sync.Mutex
	canceled bool
	listener net.Listener
	srv      *http.Server
	cancel   context.CancelFunc
	state    string
}

// NewGoogleProvider creates a new Google OAuth provider.
func NewGoogleProvider() *GoogleProvider {
	return &GoogleProvider{}
}

// Name returns the display name for the provider.
func (g *GoogleProvider) Name() string { return "Google" }

// tokenResponse is the response from the token endpoint.
type tokenResponse struct {
	accessTok  string
	refreshTok string
	expiresIn  int
	tokenType  string //nolint:unused // present for completeness with the JSON response
	errCode    string
}

func (t *tokenResponse) UnmarshalJSON(data []byte) error { //nolint:gosec // internal deserialization struct
	var raw struct {
		AccessToken  string `json:"access_token"`  //nolint:gosec // JSON field name from Google's token endpoint
		RefreshToken string `json:"refresh_token"` //nolint:gosec // JSON field name from Google's token endpoint
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.accessTok = raw.AccessToken
	t.refreshTok = raw.RefreshToken
	t.expiresIn = raw.ExpiresIn
	t.tokenType = raw.TokenType
	t.errCode = raw.Error
	return nil
}

// OAuthInitMsg is sent when the auth flow is ready and the browser should open.
type OAuthInitMsg struct {
	AuthURL string
	Port    int
}

// OAuthCompleteMsg is sent when the auth flow completes successfully.
type OAuthCompleteMsg struct {
	Token *Token
}

// OAuthErrorMsg is sent when the auth flow encounters an error.
type OAuthErrorMsg struct {
	Err error
}

// InitiateAuth binds a localhost listener and returns the auth URL.
func (g *GoogleProvider) InitiateAuth() tea.Msg {
	g.mu.Lock()
	if g.canceled {
		g.mu.Unlock()
		return OAuthErrorMsg{Err: fmt.Errorf("auth flow was canceled")}
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		g.mu.Unlock()
		return OAuthErrorMsg{Err: fmt.Errorf("bind localhost listener: %w", err)}
	}
	g.listener = ln

	// Generate CSRF state token.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		ln.Close()
		g.mu.Unlock()
		return OAuthErrorMsg{Err: fmt.Errorf("generate state: %w", err)}
	}
	g.state = hex.EncodeToString(stateBytes)
	g.mu.Unlock()

	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth2callback", port)

	authURL := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=offline&prompt=consent",
		googleAuthURL,
		googleClientID(),
		redirectURI,
		googleOAuthScope,
		g.state,
	)

	return OAuthInitMsg{
		AuthURL: authURL,
		Port:    port,
	}
}

// AwaitCallback starts an HTTP server and waits for the OAuth callback.
func (g *GoogleProvider) AwaitCallback(expiresIn int) tea.Cmd {
	g.mu.Lock()
	ln := g.listener
	expectedState := g.state
	g.mu.Unlock()

	if ln == nil {
		return func() tea.Msg {
			return OAuthErrorMsg{Err: fmt.Errorf("no listener available")}
		}
	}

	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth2callback", port)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(expiresIn)*time.Second)

	g.mu.Lock()
	g.cancel = cancel
	g.mu.Unlock()

	resultCh := make(chan tea.Msg, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2callback", newCallbackHandler(expectedState, redirectURI, resultCh))

	srv := &http.Server{Handler: mux} //nolint:gosec // localhost-only, short-lived

	g.mu.Lock()
	g.srv = srv
	g.mu.Unlock()

	return func() tea.Msg {
		defer cancel()

		// Serve in background.
		go func() {
			if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				trySend(resultCh, OAuthErrorMsg{Err: fmt.Errorf("callback server: %w", err)})
			}
		}()

		select {
		case msg := <-resultCh:
			g.shutdownServer()
			return msg
		case <-ctx.Done():
			g.shutdownServer()
			return OAuthErrorMsg{Err: fmt.Errorf("authorization timed out")}
		}
	}
}

// Cancel stops the auth flow and cleans up resources.
func (g *GoogleProvider) Cancel() tea.Msg {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.canceled = true
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	if g.srv != nil {
		g.srv.Close()
		g.srv = nil
	}
	if g.listener != nil {
		g.listener.Close()
		g.listener = nil
	}
	return nil
}

// shutdownServer performs a graceful shutdown under mutex.
func (g *GoogleProvider) shutdownServer() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		g.srv.Shutdown(ctx) //nolint:errcheck
		g.srv = nil
	}
}

// trySend performs a non-blocking send on a channel.
func trySend(ch chan<- tea.Msg, msg tea.Msg) {
	select {
	case ch <- msg:
	default:
	}
}

// newCallbackHandler returns an HTTP handler for the OAuth callback.
func newCallbackHandler(expectedState, redirectURI string, resultCh chan<- tea.Msg) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if msg := validateCallback(r, expectedState); msg != nil {
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			trySend(resultCh, msg)
			return
		}

		code := r.URL.Query().Get("code")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h2>Authentication successful!</h2><p>You can close this tab and return to Crucible.</p></body></html>`)

		trySend(resultCh, exchangeCode(redirectURI, code))
	}
}

// validateCallback checks state and error params. Returns nil on success, or
// an OAuthErrorMsg describing the problem.
func validateCallback(r *http.Request, expectedState string) tea.Msg {
	if state := r.URL.Query().Get("state"); state != expectedState {
		return OAuthErrorMsg{Err: fmt.Errorf("CSRF state mismatch")}
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		return OAuthErrorMsg{Err: fmt.Errorf("authorization denied: %s", errParam)}
	}
	if code := r.URL.Query().Get("code"); code == "" {
		return OAuthErrorMsg{Err: fmt.Errorf("missing authorization code")}
	}
	return nil
}

// exchangeCode exchanges an authorization code for tokens.
func exchangeCode(redirectURI, code string) tea.Msg {
	resp, err := http.PostForm(googleTokenURL, map[string][]string{ //nolint:noctx
		"client_id":     {googleClientID()},
		"client_secret": {googleClientSecret()},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return OAuthErrorMsg{Err: fmt.Errorf("token exchange: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthErrorMsg{Err: fmt.Errorf("reading token response: %w", err)}
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return OAuthErrorMsg{Err: fmt.Errorf("parsing token response: %w", err)}
	}

	if tr.errCode != "" {
		return OAuthErrorMsg{Err: fmt.Errorf("token error: %s", tr.errCode)}
	}

	token := &Token{
		AccessToken:  tr.accessTok,
		RefreshToken: tr.refreshTok,
		ExpiresIn:    tr.expiresIn,
	}
	token.SetExpiresAt()
	return OAuthCompleteMsg{Token: token}
}
