package config

import "os"

// AuthStatus indicates the availability of an auth method.
type AuthStatus int

const (
	// AuthStatusAvailable means the method can be selected.
	AuthStatusAvailable AuthStatus = iota
	// AuthStatusUnavailable means the method cannot be selected (missing credentials or info-only).
	AuthStatusUnavailable
	// AuthStatusCurrent means this is the currently active auth method.
	AuthStatusCurrent
)

// AuthAvailability describes a single auth method and its status.
type AuthAvailability struct {
	Method  AuthMethod
	Backend GeminiBackend
	Status  AuthStatus
	Label   string // "Gemini API Key", "Vertex AI ADC", etc.
	Detail  string // credential identifier
}

// DetectAuthMethods probes available auth methods and marks the current one.
// When activeBackend is non-empty, only methods matching that backend are returned.
func DetectAuthMethods(currentAuth AuthInfo, providerCfg ProviderConfig, activeBackend GeminiBackend) []AuthAvailability {
	all := []AuthAvailability{
		detectAPIKey(currentAuth, providerCfg),
		detectADC(currentAuth),
		detectServiceAccount(currentAuth, providerCfg),
		detectWorkloadIdentity(currentAuth),
		detectOAuth(currentAuth),
	}

	if activeBackend == "" {
		return all
	}

	filtered := make([]AuthAvailability, 0, len(all))
	for _, m := range all {
		if m.Backend == activeBackend {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func detectAPIKey(currentAuth AuthInfo, providerCfg ProviderConfig) AuthAvailability {
	key := firstNonEmpty(
		os.Getenv("GEMINI_API_KEY"),
		os.Getenv("GOOGLE_API_KEY"),
		providerCfg.APIKey,
	)
	status := AuthStatusUnavailable
	detail := "no key found"
	if key != "" {
		status = AuthStatusAvailable
		detail = MaskAPIKey(key)
	}
	if currentAuth.Method == AuthMethodAPIKey {
		status = AuthStatusCurrent
	}
	return AuthAvailability{
		Method:  AuthMethodAPIKey,
		Backend: GeminiBackendAPI,
		Status:  status,
		Label:   "Gemini API Key",
		Detail:  detail,
	}
}

func detectADC(currentAuth AuthInfo) AuthAvailability {
	status := AuthStatusUnavailable
	detail := "no ADC file found"
	if path := wellKnownADCPath(); path != "" {
		status = AuthStatusAvailable
		detail = path
	}
	if currentAuth.Method == AuthMethodUserADC {
		status = AuthStatusCurrent
	}
	return AuthAvailability{
		Method:  AuthMethodUserADC,
		Backend: GeminiBackendVertex,
		Status:  status,
		Label:   "Vertex AI ADC",
		Detail:  detail,
	}
}

func detectServiceAccount(currentAuth AuthInfo, providerCfg ProviderConfig) AuthAvailability {
	path := firstNonEmpty(
		os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		providerCfg.CredentialsFile,
	)
	status := AuthStatusUnavailable
	detail := "no credentials file"
	if path != "" {
		method, user := ReadCredentialsFile(path)
		if method == AuthMethodServiceAccount {
			status = AuthStatusAvailable
			detail = user
		}
	}
	// Always allow selecting SA (user can pick a file)
	if status == AuthStatusUnavailable {
		status = AuthStatusAvailable
		detail = "select a JSON key file"
	}
	if currentAuth.Method == AuthMethodServiceAccount {
		status = AuthStatusCurrent
	}
	return AuthAvailability{
		Method:  AuthMethodServiceAccount,
		Backend: GeminiBackendVertex,
		Status:  status,
		Label:   "Service Account",
		Detail:  detail,
	}
}

func detectWorkloadIdentity(currentAuth AuthInfo) AuthAvailability {
	email := detectMetadataIdentity()
	status := AuthStatusUnavailable
	detail := "not on GCP"
	if email != "" {
		detail = email
		if currentAuth.Method == AuthMethodWorkloadIdentity {
			status = AuthStatusCurrent
		}
	}
	return AuthAvailability{
		Method:  AuthMethodWorkloadIdentity,
		Backend: GeminiBackendVertex,
		Status:  status,
		Label:   "Workload Identity",
		Detail:  detail,
	}
}

func detectOAuth(currentAuth AuthInfo) AuthAvailability {
	status := AuthStatusAvailable
	if currentAuth.Method == AuthMethodOAuth {
		status = AuthStatusCurrent
	}
	return AuthAvailability{
		Method:  AuthMethodOAuth,
		Backend: GeminiBackendAPI,
		Status:  status,
		Label:   "Google Account (OAuth)",
		Detail:  "Google Account sign-in",
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
