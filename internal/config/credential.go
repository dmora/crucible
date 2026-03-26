package config

import "github.com/dmora/crucible/internal/oauth"

// CredentialKind identifies the authentication method.
type CredentialKind int

const (
	CredentialAPIKey         CredentialKind = iota // Gemini API via API key
	CredentialOAuth                                // Gemini API via Google account
	CredentialADC                                  // Vertex AI via gcloud ADC
	CredentialServiceAccount                       // Vertex AI via JSON key file
)

// Credential is a value type representing a resolved authentication credential.
type Credential struct {
	Kind            CredentialKind `json:"kind"`
	APIKey          string         `json:"api_key,omitempty"` //nolint:gosec // Not a hardcoded secret — user-provided credential field
	OAuthToken      *oauth.Token   `json:"oauth_token,omitempty"`
	CredentialsFile string         `json:"credentials_file,omitempty"`
}

// Backend returns the GeminiBackend implied by this credential kind.
func (c Credential) Backend() GeminiBackend {
	switch c.Kind {
	case CredentialAPIKey, CredentialOAuth:
		return GeminiBackendAPI
	case CredentialADC, CredentialServiceAccount:
		return GeminiBackendVertex
	default:
		return GeminiBackendAPI
	}
}
