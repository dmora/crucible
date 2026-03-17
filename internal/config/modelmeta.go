package config

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GeminiBackend identifies which Google AI backend to use.
type GeminiBackend string

const (
	GeminiBackendAPI    GeminiBackend = "gemini-api"
	GeminiBackendVertex GeminiBackend = "vertex-ai"
)

// AuthMethod identifies how credentials are provided to the Google AI SDK.
type AuthMethod string

const (
	AuthMethodAPIKey           AuthMethod = "API Key"
	AuthMethodServiceAccount   AuthMethod = "Service Account"
	AuthMethodUserADC          AuthMethod = "ADC"
	AuthMethodWorkloadIdentity AuthMethod = "Workload Identity"
)

// AuthInfo holds the resolved authentication state for a provider.
// Populated at model-build time from what the app actually used.
type AuthInfo struct {
	Backend  GeminiBackend // which genai backend is active
	Method   AuthMethod    // how credentials are provided
	User     string        // identity: email, service account, or masked key
	Project  string        // GCP project (Vertex only)
	Location string        // GCP location (Vertex only)
}

// HeaderDesignation returns a compact label for the header bar.
func (a AuthInfo) HeaderDesignation() string {
	switch a.Backend {
	case GeminiBackendVertex:
		switch a.Method {
		case AuthMethodServiceAccount:
			return "VERTEX:SA"
		case AuthMethodWorkloadIdentity:
			return "VERTEX:WI"
		default:
			return "VERTEX:ADC"
		}
	case GeminiBackendAPI:
		return "GEMINI:KEY"
	default:
		return "SYS:ONLINE"
	}
}

// DetectVertexAuth detects the credential method and identity that the genai SDK
// will use for Vertex AI, matching the SDK's resolution order:
//  1. GOOGLE_APPLICATION_CREDENTIALS env var → service account or user creds
//  2. Well-known ADC file (~/.config/gcloud/application_default_credentials.json)
//  3. GCE/GKE metadata server → workload identity
//  4. Fallback → gcloud CLI account
func DetectVertexAuth() (AuthMethod, string) {
	// 1. Explicit credentials file.
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		method, user := readCredentialsFile(path)
		if method != "" {
			return method, user
		}
	}

	// 2. Well-known ADC file.
	if path := wellKnownADCPath(); path != "" {
		method, user := readCredentialsFile(path)
		if method != "" {
			return method, user
		}
	}

	// 3. GCE/GKE metadata server.
	if email := detectMetadataIdentity(); email != "" {
		return AuthMethodWorkloadIdentity, email
	}

	// 4. Fallback: gcloud CLI.
	if account := detectGcloudAccount(); account != "" {
		return AuthMethodUserADC, account
	}

	return AuthMethodUserADC, ""
}

// readCredentialsFile reads a Google credentials JSON file and returns
// the auth method and identity (email).
func readCredentialsFile(path string) (AuthMethod, string) {
	data, err := os.ReadFile(path) //nolint:gosec // user-controlled path is expected
	if err != nil {
		return "", ""
	}
	var creds struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"` // service account
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", ""
	}
	switch creds.Type {
	case "service_account":
		return AuthMethodServiceAccount, creds.ClientEmail
	case "authorized_user":
		return AuthMethodUserADC, detectGcloudAccount()
	default:
		return "", ""
	}
}

// wellKnownADCPath returns the well-known ADC file path, or "" if it doesn't exist.
func wellKnownADCPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// detectMetadataIdentity checks the GCE metadata server for a service account email.
// Returns "" if not running on GCP or the metadata server is unreachable.
func detectMetadataIdentity() string {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	const metadataURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email"
	req, err := http.NewRequest(http.MethodGet, metadataURL, nil) //nolint:gosec // hardcoded GCE metadata URL, not user input
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req) //nolint:gosec,bodyclose // hardcoded GCE metadata URL; closed below
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

// detectGcloudAccount runs `gcloud config get-value account` and returns
// the active account email, or "" on any error. Timeout: 2s.
func detectGcloudAccount() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "config", "get-value", "account").Output()
	if err != nil {
		return ""
	}
	account := strings.TrimSpace(string(out))
	if account == "(unset)" {
		return ""
	}
	return account
}

// DetectGcloudAccount is the exported version for use outside config.
func DetectGcloudAccount() string {
	return detectGcloudAccount()
}

// MaskAPIKey returns a masked version of an API key showing only the last 4 chars.
func MaskAPIKey(key string) string {
	if len(key) <= 4 {
		return "···"
	}
	return "···" + key[len(key)-4:]
}

// ModelOptions stores extra options for models.
type ModelOptions struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	TopK             *int64         `json:"top_k,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	ProviderOptions  map[string]any `json:"provider_options,omitempty"`
}

// ModelMetadata represents an AI model configuration.
type ModelMetadata struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	CostPer1MIn            float64      `json:"cost_per_1m_in"`
	CostPer1MOut           float64      `json:"cost_per_1m_out"`
	CostPer1MInCached      float64      `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64      `json:"cost_per_1m_out_cached"`
	ContextWindow          int64        `json:"context_window"`
	DefaultMaxTokens       int64        `json:"default_max_tokens"`
	CanReason              bool         `json:"can_reason"`
	ReasoningLevels        []string     `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string       `json:"default_reasoning_effort,omitempty"`
	SupportsAttachments    bool         `json:"supports_attachments"`
	DisableSearch          bool         `json:"disable_search,omitempty"`
	Options                ModelOptions `json:"options"`
}

// ProviderType identifies the API format a provider uses.
type ProviderType = string

// Known provider types.
const (
	ProviderTypeGemini       ProviderType = "gemini"
	ProviderTypeOpenAI       ProviderType = "openai"
	ProviderTypeAnthropic    ProviderType = "anthropic"
	ProviderTypeOpenAICompat ProviderType = "openai-compat"
)

// ProviderMetadata represents an AI provider configuration.
type ProviderMetadata struct {
	Name                string            `json:"name"`
	ID                  string            `json:"id"`
	APIKey              string            `json:"api_key,omitempty"`
	APIEndpoint         string            `json:"api_endpoint,omitempty"`
	Type                ProviderType      `json:"type,omitempty"`
	DefaultLargeModelID string            `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string            `json:"default_small_model_id,omitempty"`
	Models              []ModelMetadata   `json:"models,omitempty"`
	DefaultHeaders      map[string]string `json:"default_headers,omitempty"`
}
