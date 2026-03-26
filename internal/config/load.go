package config

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/env"
	"github.com/dmora/crucible/internal/fsext"
	"github.com/dmora/crucible/internal/home"
	"github.com/dmora/crucible/internal/log"
	"github.com/qjebbs/go-jsons"
)

// Load loads the configuration from the default paths.
func Load(workingDir, dataDir string, debug bool) (*Config, error) {
	configPaths := lookupConfigs(workingDir)

	cfg, err := loadFromConfigPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from paths %v: %w", configPaths, err)
	}

	cfg.dataConfigDir = GlobalConfigData()
	cfg.loadedPaths = configPaths

	cfg.setDefaults(workingDir, dataDir)

	if debug {
		cfg.Options.Debug = true
	}

	// Setup logs
	log.Setup(
		filepath.Join(cfg.Options.DataDirectory, "logs", fmt.Sprintf("%s.log", appName)),
		cfg.Options.Debug,
	)

	if !isInsideWorktree() {
		const depth = 2
		const items = 100
		slog.Warn("No git repository detected in working directory, will limit file walk operations", "depth", depth, "items", items)
		assignIfNil(&cfg.Tools.Ls.MaxDepth, depth)
		assignIfNil(&cfg.Tools.Ls.MaxItems, items)
		assignIfNil(&cfg.Options.TUI.Completions.MaxDepth, depth)
		assignIfNil(&cfg.Options.TUI.Completions.MaxItems, items)
	}

	if isAppleTerminal() {
		slog.Warn("Detected Apple Terminal, enabling transparent mode")
		assignIfNil(&cfg.Options.TUI.Transparent, true)
	}

	// Load known providers
	providers, err := Providers(cfg)
	if err != nil {
		return nil, err
	}
	cfg.knownProviders = providers

	env := env.New()
	// Configure providers
	valueResolver := NewShellVariableResolver(env)
	cfg.resolver = valueResolver
	if err := cfg.configureProviders(env, valueResolver, cfg.knownProviders); err != nil {
		return nil, fmt.Errorf("failed to configure providers: %w", err)
	}

	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("no authentication configured — set GEMINI_API_KEY (or GOOGLE_API_KEY) for Gemini API, or GOOGLE_CLOUD_PROJECT for Vertex AI")
	}

	if err := cfg.configureSelectedModels(cfg.knownProviders); err != nil {
		return nil, fmt.Errorf("failed to configure selected models: %w", err)
	}
	if err := cfg.MCP.ValidateMCPNames(); err != nil {
		return nil, err
	}
	cfg.SetupAgents()
	cfg.SetupDefaultStations()
	return cfg, nil
}

// crucibleEnvAllowlist contains the env var suffixes (after stripping "CRUCIBLE_")
// that PushPopCrucibleEnv is allowed to override. This prevents CRUCIBLE_PATH,
// CRUCIBLE_HOME, or other sensitive variables from being set unexpectedly.
var crucibleEnvAllowlist = map[string]bool{
	"GEMINI_API_KEY":    true,
	"OPENAI_API_KEY":    true,
	"ANTHROPIC_API_KEY": true,
}

func PushPopCrucibleEnv() func() {
	var found []string
	for _, ev := range os.Environ() {
		if strings.HasPrefix(ev, "CRUCIBLE_") {
			pair := strings.SplitN(ev, "=", 2)
			if len(pair) != 2 {
				continue
			}
			suffix := strings.TrimPrefix(pair[0], "CRUCIBLE_")
			if !crucibleEnvAllowlist[suffix] {
				continue
			}
			found = append(found, suffix)
		}
	}
	backups := make(map[string]string)
	for _, ev := range found {
		backups[ev] = os.Getenv(ev)
	}

	for _, ev := range found {
		os.Setenv(ev, os.Getenv("CRUCIBLE_"+ev))
	}

	restore := func() {
		for k, v := range backups {
			os.Setenv(k, v)
		}
	}
	return restore
}

var knownProviderTypes = []ProviderType{
	ProviderTypeGemini,
	ProviderTypeOpenAI,
	ProviderTypeAnthropic,
	ProviderTypeOpenAICompat,
}

func (c *Config) configureProviders(env env.Env, resolver VariableResolver, knownProviders []ProviderMetadata) error {
	knownProviderNames := make(map[string]bool)
	restore := PushPopCrucibleEnv()
	defer restore()

	// When disable_default_providers is enabled, skip all default/embedded
	// providers entirely. Users must fully specify any providers they want.
	if c.Options.DisableDefaultProviders {
		knownProviders = nil
	}

	for _, p := range knownProviders {
		knownProviderNames[p.ID] = true
		config, configExists := c.Providers.Get(p.ID)
		if configExists {
			if config.BaseURL != "" {
				p.APIEndpoint = config.BaseURL
			}
			if config.APIKey != "" {
				p.APIKey = config.APIKey
			}
			if len(config.Models) > 0 {
				models := []ModelMetadata{}
				seen := make(map[string]bool)

				for _, model := range config.Models {
					if seen[model.ID] {
						continue
					}
					seen[model.ID] = true
					if model.Name == "" {
						model.Name = model.ID
					}
					models = append(models, model)
				}
				for _, model := range p.Models {
					if seen[model.ID] {
						continue
					}
					seen[model.ID] = true
					if model.Name == "" {
						model.Name = model.ID
					}
					models = append(models, model)
				}

				p.Models = models
			}
		}

		headers := map[string]string{}
		if len(p.DefaultHeaders) > 0 {
			maps.Copy(headers, p.DefaultHeaders)
		}
		if len(config.ExtraHeaders) > 0 {
			maps.Copy(headers, config.ExtraHeaders)
		}
		for k, v := range headers {
			resolved, err := resolver.ResolveValue(v)
			if err != nil {
				slog.Error("Could not resolve provider header", "err", err.Error())
				continue
			}
			headers[k] = resolved
		}
		// Resolve API key. The genai SDK prefers GOOGLE_API_KEY over GEMINI_API_KEY,
		// so check both to match SDK behavior. Only apply the GOOGLE_API_KEY
		// fallback to the real Gemini provider (ID "gemini"), not Vertex AI —
		// Vertex requires explicit Vertex signals (GOOGLE_CLOUD_PROJECT / ADC).
		resolvedAPIKey, _ := resolver.ResolveValue(p.APIKey)
		if resolvedAPIKey == "" && p.ID == "gemini" {
			resolvedAPIKey = env.Get("GOOGLE_API_KEY")
		}

		// Resolve backend: explicit config > env-based auto-detection.
		// Only Gemini-type providers can auto-detect Vertex AI from GOOGLE_CLOUD_PROJECT.
		// Non-Gemini providers (OpenAI-compat, Anthropic, etc.) require an explicit API key.
		// Within Gemini-type, the "gemini" provider (API-key based) also skips env detection
		// unless explicitly configured — the "vertexai" provider is canonical for Vertex AI.
		skipEnvDetect := p.Type != ProviderTypeGemini ||
			(p.ID == "gemini" && !configExists)
		backend, project, location := resolveBackend(config.Backend, config.Project, config.Location, resolvedAPIKey, env, skipEnvDetect)

		// Skip providers with no usable auth — but keep those using OAuth,
		// service account credentials, or a Credential value type.
		hasAltAuth := config.OAuthToken != nil || config.CredentialsFile != "" || config.Credential != (Credential{})
		if resolvedAPIKey == "" && backend != GeminiBackendVertex && !hasAltAuth {
			if configExists {
				slog.Warn("Skipping provider due to missing API key", "provider", p.ID)
				c.Providers.Del(p.ID)
			}
			continue
		}

		// Reflect the actual backend in the provider display name (Gemini only).
		providerName := p.Name
		if backend == GeminiBackendVertex && p.Type == ProviderTypeGemini {
			providerName = "Google Vertex AI"
		}

		prepared := ProviderConfig{
			ID:                 p.ID,
			Name:               providerName,
			BaseURL:            p.APIEndpoint,
			APIKey:             resolvedAPIKey,
			APIKeyTemplate:     p.APIKey,
			OAuthToken:         config.OAuthToken,
			CredentialsFile:    config.CredentialsFile,
			Credential:         config.Credential,
			Type:               p.Type,
			Disable:            config.Disable,
			SystemPromptPrefix: config.SystemPromptPrefix,
			ExtraHeaders:       headers,
			ExtraBody:          config.ExtraBody,
			ExtraParams:        make(map[string]string),
			Models:             p.Models,
			Backend:            backend,
			Project:            project,
			Location:           location,
		}
		c.Providers.Set(p.ID, prepared)
	}

	// validate the custom providers
	for id, providerConfig := range c.Providers.Seq2() {
		if knownProviderNames[id] {
			continue
		}

		providerConfig.ID = id
		providerConfig.Name = cmp.Or(providerConfig.Name, id)
		providerConfig.Type = cmp.Or(providerConfig.Type, ProviderTypeOpenAICompat)
		if !slices.Contains(knownProviderTypes, providerConfig.Type) {
			slog.Warn("Skipping custom provider due to unsupported provider type", "provider", id)
			c.Providers.Del(id)
			continue
		}

		if providerConfig.Disable {
			slog.Debug("Skipping custom provider due to disable flag", "provider", id)
			c.Providers.Del(id)
			continue
		}
		if providerConfig.APIKey == "" {
			slog.Warn("Provider is missing API key, this might be OK for local providers", "provider", id)
		}
		if providerConfig.BaseURL == "" {
			slog.Warn("Skipping custom provider due to missing API endpoint", "provider", id)
			c.Providers.Del(id)
			continue
		}
		if len(providerConfig.Models) == 0 {
			slog.Warn("Skipping custom provider because the provider has no models", "provider", id)
			c.Providers.Del(id)
			continue
		}
		apiKey, err := resolver.ResolveValue(providerConfig.APIKey)
		if apiKey == "" || err != nil {
			slog.Warn("Provider is missing API key, this might be OK for local providers", "provider", id)
		}
		baseURL, err := resolver.ResolveValue(providerConfig.BaseURL)
		if baseURL == "" || err != nil {
			slog.Warn("Skipping custom provider due to missing API endpoint", "provider", id, "error", err)
			c.Providers.Del(id)
			continue
		}

		for k, v := range providerConfig.ExtraHeaders {
			resolved, err := resolver.ResolveValue(v)
			if err != nil {
				slog.Error("Could not resolve provider header", "err", err.Error())
				continue
			}
			providerConfig.ExtraHeaders[k] = resolved
		}

		c.Providers.Set(id, providerConfig)
	}

	if c.Providers.Len() == 0 && c.Options.DisableDefaultProviders {
		return fmt.Errorf("default providers are disabled and there are no custom providers are configured")
	}

	return nil
}

func (c *Config) setDefaults(workingDir, dataDir string) {
	c.workingDir = workingDir
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	if dataDir != "" {
		c.Options.DataDirectory = dataDir
	} else if c.Options.DataDirectory == "" {
		if path, ok := fsext.LookupClosest(workingDir, defaultDataDirectory); ok {
			c.Options.DataDirectory = path
		} else {
			c.Options.DataDirectory = filepath.Join(workingDir, defaultDataDirectory)
		}
	}
	if c.Providers == nil {
		c.Providers = csync.NewMap[string, ProviderConfig]()
	}
	if c.Models == nil {
		c.Models = make(map[SelectedModelType]SelectedModel)
	}
	if c.RecentModels == nil {
		c.RecentModels = make(map[SelectedModelType][]SelectedModel)
	}
	if c.MCP == nil {
		c.MCP = make(map[string]MCPConfig)
	}
	if c.LSP == nil {
		c.LSP = make(map[string]LSPConfig)
	}

	// User-configured context paths are kept separate from tiered defaults.
	// Tiered defaults are resolved at prompt assembly time in promptData().
	// Only deduplicate user-configured paths here.
	if len(c.Options.ContextPaths) > 0 {
		slices.Sort(c.Options.ContextPaths)
		c.Options.ContextPaths = slices.Compact(c.Options.ContextPaths)
	}

	// Add the default skills directories if not already present.
	for _, dir := range GlobalSkillsDirs() {
		if !slices.Contains(c.Options.SkillsPaths, dir) {
			c.Options.SkillsPaths = append(c.Options.SkillsPaths, dir)
		}
	}

	if str, ok := os.LookupEnv("CRUCIBLE_DISABLE_PROVIDER_AUTO_UPDATE"); ok {
		c.Options.DisableProviderAutoUpdate, _ = strconv.ParseBool(str)
	}

	if str, ok := os.LookupEnv("CRUCIBLE_DISABLE_DEFAULT_PROVIDERS"); ok {
		c.Options.DisableDefaultProviders, _ = strconv.ParseBool(str)
	}

	if c.Options.Attribution == nil {
		c.Options.Attribution = &Attribution{
			TrailerStyle:  TrailerStyleAssistedBy,
			GeneratedWith: true,
		}
	} else if c.Options.Attribution.TrailerStyle == "" {
		// Migrate deprecated co_authored_by or apply default
		if c.Options.Attribution.CoAuthoredBy != nil {
			if *c.Options.Attribution.CoAuthoredBy {
				c.Options.Attribution.TrailerStyle = TrailerStyleCoAuthoredBy
			} else {
				c.Options.Attribution.TrailerStyle = TrailerStyleNone
			}
		} else {
			c.Options.Attribution.TrailerStyle = TrailerStyleAssistedBy
		}
	}
	c.Options.InitializeAs = cmp.Or(c.Options.InitializeAs, defaultInitializeAs)
}

func (c *Config) defaultModelSelection(knownProviders []ProviderMetadata) (largeModel SelectedModel, smallModel SelectedModel, err error) {
	if len(knownProviders) == 0 && c.Providers.Len() == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	// Use the first provider enabled based on the known providers order
	// if no provider found that is known use the first provider configured
	for _, p := range knownProviders {
		providerConfig, ok := c.Providers.Get(p.ID)
		if !ok || providerConfig.Disable {
			continue
		}
		defaultLargeModel := c.GetModel(p.ID, p.DefaultLargeModelID)
		if defaultLargeModel == nil {
			err = fmt.Errorf("default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			return largeModel, smallModel, err
		}
		largeModel = SelectedModel{
			Provider:        p.ID,
			Model:           defaultLargeModel.ID,
			MaxTokens:       defaultLargeModel.DefaultMaxTokens,
			ReasoningEffort: defaultLargeModel.DefaultReasoningEffort,
		}

		defaultSmallModel := c.GetModel(p.ID, p.DefaultSmallModelID)
		if defaultSmallModel == nil {
			err = fmt.Errorf("default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			return largeModel, smallModel, err
		}
		smallModel = SelectedModel{
			Provider:        p.ID,
			Model:           defaultSmallModel.ID,
			MaxTokens:       defaultSmallModel.DefaultMaxTokens,
			ReasoningEffort: defaultSmallModel.DefaultReasoningEffort,
		}
		return largeModel, smallModel, err
	}

	enabledProviders := c.EnabledProviders()
	slices.SortFunc(enabledProviders, func(a, b ProviderConfig) int {
		return strings.Compare(a.ID, b.ID)
	})

	if len(enabledProviders) == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	providerConfig := enabledProviders[0]
	if len(providerConfig.Models) == 0 {
		err = fmt.Errorf("provider %s has no models configured", providerConfig.ID)
		return largeModel, smallModel, err
	}
	defaultLargeModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	largeModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultLargeModel.ID,
		MaxTokens: defaultLargeModel.DefaultMaxTokens,
	}
	defaultSmallModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	smallModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultSmallModel.ID,
		MaxTokens: defaultSmallModel.DefaultMaxTokens,
	}
	return largeModel, smallModel, err
}

func (c *Config) configureSelectedModels(knownProviders []ProviderMetadata) error {
	defaultLarge, defaultSmall, err := c.defaultModelSelection(knownProviders)
	if err != nil {
		return fmt.Errorf("failed to select default models: %w", err)
	}

	large, err := c.resolveModel(SelectedModelTypeLarge, defaultLarge)
	if err != nil {
		return err
	}
	small, err := c.resolveModel(SelectedModelTypeSmall, defaultSmall)
	if err != nil {
		return err
	}

	c.Models[SelectedModelTypeLarge] = large
	c.Models[SelectedModelTypeSmall] = small
	return nil
}

// resolveModel merges user-selected model options into the default for a given model type.
// If the selected model/provider combo is not found, it falls back to the default.
func (c *Config) resolveModel(modelType SelectedModelType, defaultModel SelectedModel) (SelectedModel, error) {
	selected, configured := c.Models[modelType]
	if !configured {
		return defaultModel, nil
	}

	result := defaultModel
	if selected.Model != "" {
		result.Model = selected.Model
	}
	if selected.Provider != "" {
		result.Provider = selected.Provider
	}

	model := c.GetModel(result.Provider, result.Model)
	if model == nil {
		if err := c.UpdatePreferredModel(modelType, defaultModel); err != nil {
			return SelectedModel{}, fmt.Errorf("failed to update preferred %s model: %w", modelType, err)
		}
		return defaultModel, nil
	}

	if selected.MaxTokens > 0 {
		result.MaxTokens = selected.MaxTokens
	} else {
		result.MaxTokens = model.DefaultMaxTokens
	}
	if selected.ReasoningEffort != "" {
		result.ReasoningEffort = selected.ReasoningEffort
	}
	result.Think = selected.Think
	if selected.Temperature != nil {
		result.Temperature = selected.Temperature
	}
	if selected.TopP != nil {
		result.TopP = selected.TopP
	}
	if selected.TopK != nil {
		result.TopK = selected.TopK
	}
	if selected.FrequencyPenalty != nil {
		result.FrequencyPenalty = selected.FrequencyPenalty
	}
	if selected.PresencePenalty != nil {
		result.PresencePenalty = selected.PresencePenalty
	}
	return result, nil
}

// lookupConfigs searches config files recursively from CWD up to FS root
func lookupConfigs(cwd string) []string {
	// prepend default config paths
	configPaths := []string{
		GlobalConfig(),
		GlobalConfigData(),
	}

	configNames := []string{appName + ".json", "." + appName + ".json"}

	foundConfigs, err := fsext.Lookup(cwd, configNames...)
	if err != nil {
		// returns at least default configs
		return configPaths
	}

	// reverse order so last config has more priority
	slices.Reverse(foundConfigs)

	return append(configPaths, foundConfigs...)
}

// stationDefaultsJSON marshals DefaultStations into a JSON byte slice
// suitable for use as the lowest-priority layer in the config merge chain.
func stationDefaultsJSON() []byte {
	type wrapper struct {
		Stations map[string]StationConfig `json:"stations"`
	}
	data, err := json.Marshal(wrapper{Stations: DefaultStations})
	if err != nil {
		slog.Error("Failed to marshal default stations", "err", err)
		return []byte("{}")
	}
	return data
}

func loadFromConfigPaths(configPaths []string) (*Config, error) {
	// Prepend station defaults as the lowest-priority layer.
	// The jsons.Merge deep merge ensures user fields override defaults
	// while unset fields inherit from defaults (field-level merge).
	configs := [][]byte{stationDefaultsJSON()}

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to open config file %s: %w", path, err)
		}
		if len(data) == 0 {
			continue
		}
		configs = append(configs, data)
	}

	return loadFromBytes(configs)
}

func loadFromBytes(configs [][]byte) (*Config, error) {
	if len(configs) == 0 {
		return &Config{}, nil
	}

	data, err := jsons.Merge(configs)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// GlobalConfig returns the global configuration file path for the application.
func GlobalConfig() string {
	if crucibleGlobal := os.Getenv("CRUCIBLE_GLOBAL_CONFIG"); crucibleGlobal != "" {
		return filepath.Join(crucibleGlobal, fmt.Sprintf("%s.json", appName))
	}
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, appName, fmt.Sprintf("%s.json", appName))
	}
	return filepath.Join(home.Dir(), ".config", appName, fmt.Sprintf("%s.json", appName))
}

// GlobalConfigData returns the path to the main data directory for the application.
// this config is used when the app overrides configurations instead of updating the global config.
func GlobalConfigData() string {
	if crucibleData := os.Getenv("CRUCIBLE_GLOBAL_DATA"); crucibleData != "" {
		return filepath.Join(crucibleData, fmt.Sprintf("%s.json", appName))
	}
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName, fmt.Sprintf("%s.json", appName))
	}

	// return the path to the main data directory
	// for windows, it should be in `%LOCALAPPDATA%/crucible/``
	// for linux and macOS, it should be in `$HOME/.local/share/crucible/``
	if runtime.GOOS == "windows" {
		localAppData := cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
		return filepath.Join(localAppData, appName, fmt.Sprintf("%s.json", appName))
	}

	return filepath.Join(home.Dir(), ".local", "share", appName, fmt.Sprintf("%s.json", appName))
}

func assignIfNil[T any](ptr **T, val T) {
	if *ptr == nil {
		*ptr = &val
	}
}

func isInsideWorktree() bool {
	bts, err := exec.CommandContext(
		context.Background(),
		"git", "rev-parse",
		"--is-inside-work-tree",
	).CombinedOutput()
	return err == nil && strings.TrimSpace(string(bts)) == "true"
}

// GlobalSkillsDirs returns the default directories for Agent Skills.
// Skills in these directories are auto-discovered and their files can be read
// without permission prompts.
func GlobalSkillsDirs() []string {
	if crucibleSkills := os.Getenv("CRUCIBLE_SKILLS_DIR"); crucibleSkills != "" {
		return []string{crucibleSkills}
	}

	// Determine the base config directory.
	var configBase string
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		configBase = xdgConfigHome
	} else if runtime.GOOS == "windows" {
		configBase = cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
	} else {
		configBase = filepath.Join(home.Dir(), ".config")
	}

	return []string{
		filepath.Join(configBase, appName, "skills"),
		filepath.Join(configBase, "agents", "skills"),
	}
}

func isAppleTerminal() bool { return os.Getenv("TERM_PROGRAM") == "Apple_Terminal" }

// resolveBackend determines the Google AI backend from explicit config, then env vars.
// Priority: explicit config > API key presence > GOOGLE_CLOUD_PROJECT env var.
// Returns the resolved backend, project, and location.
func resolveBackend(cfgBackend GeminiBackend, cfgProject, cfgLocation, resolvedAPIKey string, e env.Env, skipEnvDetect ...bool) (GeminiBackend, string, string) {
	const defaultLocation = "global"

	// Explicit config wins
	if cfgBackend != "" {
		if cfgBackend == GeminiBackendVertex {
			project := cmp.Or(cfgProject, e.Get("GOOGLE_CLOUD_PROJECT"))
			location := cmp.Or(cfgLocation, e.Get("GOOGLE_CLOUD_LOCATION"), defaultLocation)
			return GeminiBackendVertex, project, location
		}
		return GeminiBackendAPI, "", ""
	}

	// Auto-detect: API key present → Gemini API
	if resolvedAPIKey != "" {
		return GeminiBackendAPI, "", ""
	}

	// Auto-detect: GCP project set → Vertex AI (skipped when caller opts out)
	if len(skipEnvDetect) > 0 && skipEnvDetect[0] {
		return "", "", ""
	}
	if project := e.Get("GOOGLE_CLOUD_PROJECT"); project != "" {
		location := cmp.Or(cfgLocation, e.Get("GOOGLE_CLOUD_LOCATION"), defaultLocation)
		return GeminiBackendVertex, project, location
	}

	// No auth signals — return empty backend, caller decides
	return "", "", ""
}
