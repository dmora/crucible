package config

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/env"
	"github.com/dmora/crucible/internal/oauth"
	"github.com/invopop/jsonschema"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	appName              = "crucible"
	defaultDataDirectory = ".crucible"
	defaultInitializeAs  = "CLAUDE.md"
)

// ContextTier groups context file paths that share a priority level.
// During prompt assembly, the first tier with any existing file wins —
// all files from that tier are loaded, and lower tiers are skipped.
type ContextTier struct {
	Paths []string
}

// defaultContextTiers defines the exclusive fallback chain for context files.
// Order matters: Tier 1 wins over Tier 2, Tier 2 over Tier 3, etc.
var defaultContextTiers = []ContextTier{
	// Tier 1: Crucible-specific project context
	{Paths: []string{
		"CRUCIBLE.md", "CRUCIBLE.local.md",
		"crucible.md", "crucible.local.md",
		"Crucible.md", "Crucible.local.md",
	}},
	// Tier 2: AGENTS.md — cross-tool agent context
	{Paths: []string{
		"AGENTS.md", "agents.md", "Agents.md",
	}},
	// Tier 3: Claude-specific context
	{Paths: []string{
		"CLAUDE.md", "claude.md",
	}},
	// Tier 4: Community standards / other tools
	{Paths: []string{
		".github/copilot-instructions.md",
		".cursorrules",
		".cursor/rules/",
		"GEMINI.md", "gemini.md",
	}},
}

// FlattenContextTiers returns all paths from all tiers as a flat slice.
// Used by contextPathsExist() for tier-agnostic "does any exist?" checking.
func FlattenContextTiers() []string {
	n := 0
	for _, tier := range defaultContextTiers {
		n += len(tier.Paths)
	}
	all := make([]string, 0, n)
	for _, tier := range defaultContextTiers {
		all = append(all, tier.Paths...)
	}
	return all
}

// DefaultContextTiers returns the tiered context path definitions.
// Used by prompt assembly for tier-aware resolution.
func DefaultContextTiers() []ContextTier {
	return defaultContextTiers
}

// defaultContextPaths is the flattened list of all tiered context paths.
// Used by contextPathsExist() for init detection (tier-agnostic).
var defaultContextPaths = FlattenContextTiers()

type SelectedModelType string

// String returns the string representation of the [SelectedModelType].
func (s SelectedModelType) String() string {
	return string(s)
}

const (
	SelectedModelTypeLarge SelectedModelType = "large"
	SelectedModelTypeSmall SelectedModelType = "small"
)

// AgentCrucible is the primary agent identifier.
const AgentCrucible string = "crucible"

type SelectedModel struct {
	// The model id as used by the provider API.
	// Required.
	Model string `json:"model" jsonschema:"required,description=The model ID as used by the provider API,example=gpt-4o"`
	// The model provider, same as the key/id used in the providers config.
	// Required.
	Provider string `json:"provider" jsonschema:"required,description=The model provider ID that matches a key in the providers config,example=openai"`

	// Only used by models that use the openai provider and need this set.
	ReasoningEffort string `json:"reasoning_effort,omitempty" jsonschema:"description=Reasoning effort level for OpenAI models that support it,enum=low,enum=medium,enum=high"`

	// Used by anthropic models that can reason to indicate if the model should think.
	Think bool `json:"think,omitempty" jsonschema:"description=Enable thinking mode for Anthropic models that support reasoning"`

	// Overrides the default model configuration.
	MaxTokens        int64    `json:"max_tokens,omitempty" jsonschema:"description=Maximum number of tokens for model responses,maximum=200000,example=4096"`
	Temperature      *float64 `json:"temperature,omitempty" jsonschema:"description=Sampling temperature,minimum=0,maximum=1,example=0.7"`
	TopP             *float64 `json:"top_p,omitempty" jsonschema:"description=Top-p (nucleus) sampling parameter,minimum=0,maximum=1,example=0.9"`
	TopK             *int64   `json:"top_k,omitempty" jsonschema:"description=Top-k sampling parameter"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty" jsonschema:"description=Frequency penalty to reduce repetition"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty" jsonschema:"description=Presence penalty to increase topic diversity"`

	// Override provider specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for the model"`
}

type ProviderConfig struct {
	// The provider's id.
	ID string `json:"id,omitempty" jsonschema:"description=Unique identifier for the provider,example=openai"`
	// The provider's name, used for display purposes.
	Name string `json:"name,omitempty" jsonschema:"description=Human-readable name for the provider,example=OpenAI"`
	// The provider's API endpoint.
	BaseURL string `json:"base_url,omitempty" jsonschema:"description=Base URL for the provider's API,format=uri,example=https://api.openai.com/v1"`
	// The provider type, e.g. "gemini", "openai", "anthropic", etc. if empty it defaults to gemini.
	Type ProviderType `json:"type,omitempty" jsonschema:"description=Provider type that determines the API format,enum=gemini,enum=openai,enum=anthropic,enum=openai-compat,default=gemini"`
	// The provider's API key.
	APIKey string `json:"api_key,omitempty" jsonschema:"description=API key for authentication with the provider,example=$OPENAI_API_KEY"`
	// The original API key template before resolution (for re-resolution on auth errors).
	APIKeyTemplate string `json:"-"`
	// OAuthToken for providers that use OAuth2 authentication.
	OAuthToken *oauth.Token `json:"oauth,omitempty" jsonschema:"description=OAuth2 token for authentication with the provider"`

	// Backend selects the Google AI backend: "gemini-api" (default) or "vertex-ai".
	// If empty, auto-detected from env vars (GEMINI_API_KEY → gemini-api, GOOGLE_CLOUD_PROJECT → vertex-ai).
	Backend GeminiBackend `json:"backend,omitempty" jsonschema:"description=Google AI backend to use,enum=gemini-api,enum=vertex-ai"`
	// Project is the GCP project ID (required for Vertex AI, ignored for Gemini API).
	Project string `json:"project,omitempty" jsonschema:"description=GCP project ID for Vertex AI"`
	// Location is the GCP region (Vertex AI only, defaults to us-central1).
	Location string `json:"location,omitempty" jsonschema:"description=GCP location for Vertex AI,default=us-central1"`
	// Marks the provider as disabled.
	Disable bool `json:"disable,omitempty" jsonschema:"description=Whether this provider is disabled,default=false"`

	// Custom system prompt prefix.
	SystemPromptPrefix string `json:"system_prompt_prefix,omitempty" jsonschema:"description=Custom prefix to add to system prompts for this provider"`

	// Extra headers to send with each request to the provider.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty" jsonschema:"description=Additional HTTP headers to send with requests"`
	// Extra body
	ExtraBody map[string]any `json:"extra_body,omitempty" jsonschema:"description=Additional fields to include in request bodies, only works with openai-compatible providers"`

	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for this provider"`

	// Used to pass extra parameters to the provider.
	ExtraParams map[string]string `json:"-"`

	// The provider models
	Models []ModelMetadata `json:"models,omitempty" jsonschema:"description=List of models available from this provider"`
}

// ToProviderMetadata converts a ProviderConfig to a ProviderMetadata.
// The id parameter is the provider key from the config map.
func (p ProviderConfig) ToProviderMetadata(id string) ProviderMetadata {
	pid := p.ID
	if pid == "" {
		pid = id
	}
	name := p.Name
	if name == "" {
		name = id
	}
	return ProviderMetadata{
		ID:          pid,
		Name:        name,
		Type:        p.Type,
		APIEndpoint: p.BaseURL,
		Models:      p.Models,
	}
}

type MCPType string

const (
	MCPStdio MCPType = "stdio"
	MCPSSE   MCPType = "sse"
	MCPHttp  MCPType = "http"
)

type MCPConfig struct {
	Command       string            `json:"command,omitempty" jsonschema:"description=Command to execute for stdio MCP servers,example=npx"`
	Env           map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set for the MCP server"`
	Args          []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the MCP server command"`
	Type          MCPType           `json:"type" jsonschema:"required,description=Type of MCP connection,enum=stdio,enum=sse,enum=http,default=stdio"`
	URL           string            `json:"url,omitempty" jsonschema:"description=URL for HTTP or SSE MCP servers,format=uri,example=http://localhost:3000/mcp"`
	Disabled      bool              `json:"disabled,omitempty" jsonschema:"description=Whether this MCP server is disabled,default=false"`
	DisabledTools []string          `json:"disabled_tools,omitempty" jsonschema:"description=List of tools from this MCP server to disable,example=get-library-doc"`
	Timeout       int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for MCP server connections,default=15,example=30,example=60,example=120"`

	// TODO: maybe make it possible to get the value from the env
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers for HTTP/SSE MCP servers"`
}

type LSPConfig struct {
	Disabled    bool              `json:"disabled,omitempty" jsonschema:"description=Whether this LSP server is disabled,default=false"`
	Command     string            `json:"command,omitempty" jsonschema:"description=Command to execute for the LSP server,example=gopls"`
	Args        []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the LSP server command"`
	Env         map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set to the LSP server command"`
	FileTypes   []string          `json:"filetypes,omitempty" jsonschema:"description=File types this LSP server handles,example=go,example=mod,example=rs,example=c,example=js,example=ts"`
	RootMarkers []string          `json:"root_markers,omitempty" jsonschema:"description=Files or directories that indicate the project root,example=go.mod,example=package.json,example=Cargo.toml"`
	InitOptions map[string]any    `json:"init_options,omitempty" jsonschema:"description=Initialization options passed to the LSP server during initialize request"`
	Options     map[string]any    `json:"options,omitempty" jsonschema:"description=LSP server-specific settings passed during initialization"`
	Timeout     int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for LSP server initialization,default=30,example=60,example=120"`
}

type TUIOptions struct {
	CompactMode bool   `json:"compact_mode,omitempty" jsonschema:"description=Enable compact mode for the TUI interface,default=false"`
	DiffMode    string `json:"diff_mode,omitempty" jsonschema:"description=Diff mode for the TUI interface,enum=unified,enum=split"`
	Theme       string `json:"theme,omitempty" jsonschema:"description=UI color theme,enum=steel-blue,enum=amber-forge,enum=phosphor-green,enum=reactor-red,enum=titanium,enum=clean-room,default=steel-blue"`
	Spinner     string `json:"spinner,omitempty" jsonschema:"title=Spinner style,enum=industrial,enum=pulse,enum=dots,enum=ellipsis,enum=points,enum=meter,enum=hamburger,enum=trigram,default=meter"`

	Completions Completions `json:"completions,omitzero" jsonschema:"description=Completions UI options"`
	Transparent *bool       `json:"transparent,omitempty" jsonschema:"description=Enable transparent background for the TUI interface,default=false"`
}

// Completions defines options for the completions UI.
type Completions struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

func (c Completions) Limits() (depth, items int) {
	return ptrValOr(c.MaxDepth, 0), ptrValOr(c.MaxItems, 0)
}

type Permissions struct {
	AllowedTools []string `json:"allowed_tools,omitempty" jsonschema:"description=List of tools that don't require permission prompts,example=bash,example=view"` // Tools that don't require permission prompts
	SkipRequests bool     `json:"-"`                                                                                                                              // Automatically accept all permissions (YOLO mode)
}

type TrailerStyle string

const (
	TrailerStyleNone         TrailerStyle = "none"
	TrailerStyleCoAuthoredBy TrailerStyle = "co-authored-by"
	TrailerStyleAssistedBy   TrailerStyle = "assisted-by"
)

type Attribution struct {
	TrailerStyle  TrailerStyle `json:"trailer_style,omitempty" jsonschema:"description=Style of attribution trailer to add to commits,enum=none,enum=co-authored-by,enum=assisted-by,default=assisted-by"`
	CoAuthoredBy  *bool        `json:"co_authored_by,omitempty" jsonschema:"description=Deprecated: use trailer_style instead"`
	GeneratedWith bool         `json:"generated_with,omitempty" jsonschema:"description=Add Generated with Crucible line to commit messages and issues and PRs,default=true"`
}

// JSONSchemaExtend marks the co_authored_by field as deprecated in the schema.
func (Attribution) JSONSchemaExtend(schema *jsonschema.Schema) {
	if schema.Properties != nil {
		if prop, ok := schema.Properties.Get("co_authored_by"); ok {
			prop.Deprecated = true
		}
	}
}

type Options struct {
	ContextPaths              []string     `json:"context_paths,omitempty" jsonschema:"description=Paths to files containing context information for the AI,example=.cursorrules,example=CRUCIBLE.md"`
	SkillsPaths               []string     `json:"skills_paths,omitempty" jsonschema:"description=Paths to directories containing Agent Skills (folders with SKILL.md files),example=~/.config/crucible/skills,example=./skills"`
	TUI                       *TUIOptions  `json:"tui,omitempty" jsonschema:"description=Terminal user interface options"`
	Debug                     bool         `json:"debug,omitempty" jsonschema:"description=Enable debug logging,default=false"`
	DisableAutoSummarize      bool         `json:"disable_auto_summarize,omitempty" jsonschema:"description=Disable automatic conversation summarization,default=false"`
	DataDirectory             string       `json:"data_directory,omitempty" jsonschema:"description=Directory for storing application data (relative to working directory),default=.crucible,example=.crucible"` // Relative to the cwd
	DisabledTools             []string     `json:"disabled_tools,omitempty" jsonschema:"description=List of built-in tools to disable and hide from the agent,example=bash,example=sourcegraph"`
	DisableProviderAutoUpdate bool         `json:"disable_provider_auto_update,omitempty" jsonschema:"description=Disable providers auto-update,default=false"`
	DisableDefaultProviders   bool         `json:"disable_default_providers,omitempty" jsonschema:"description=Ignore all default/embedded providers. When enabled, providers must be fully specified in the config file with base_url, models, and api_key - no merging with defaults occurs,default=false"`
	Attribution               *Attribution `json:"attribution,omitempty" jsonschema:"description=Attribution settings for generated content"`
	DisableMetrics            bool         `json:"disable_metrics,omitempty" jsonschema:"description=Disable sending metrics,default=false"`
	InitializeAs              string       `json:"initialize_as,omitempty" jsonschema:"description=Name of the context file to create/update during project initialization,default=CLAUDE.md,example=CLAUDE.md,example=CRUCIBLE.md,example=AGENTS.md,example=docs/LLMs.md"`
	Progress                  *bool        `json:"progress,omitempty" jsonschema:"description=Show indeterminate progress updates during long operations,default=true"`
	Worktree                  bool         `json:"worktree,omitempty" jsonschema:"description=Enable git worktree isolation per session,default=false"`
}

type MCPs map[string]MCPConfig

type MCP struct {
	Name string    `json:"name"`
	MCP  MCPConfig `json:"mcp"`
}

// ValidateMCPNames checks that MCP server names are safe for the
// mcp_<server>_<tool> naming convention. Names must not contain underscores.
func (m MCPs) ValidateMCPNames() error {
	for name := range m {
		if strings.Contains(name, "_") {
			return fmt.Errorf(
				"MCP server name %q contains underscores; "+
					"use hyphens instead (e.g. %q) to avoid tool name collisions",
				name, strings.ReplaceAll(name, "_", "-"),
			)
		}
	}
	return nil
}

func (m MCPs) Sorted() []MCP {
	sorted := make([]MCP, 0, len(m))
	for k, v := range m {
		sorted = append(sorted, MCP{
			Name: k,
			MCP:  v,
		})
	}
	slices.SortFunc(sorted, func(a, b MCP) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}

type LSPs map[string]LSPConfig

type LSP struct {
	Name string    `json:"name"`
	LSP  LSPConfig `json:"lsp"`
}

func (l LSPs) Sorted() []LSP {
	sorted := make([]LSP, 0, len(l))
	for k, v := range l {
		sorted = append(sorted, LSP{
			Name: k,
			LSP:  v,
		})
	}
	slices.SortFunc(sorted, func(a, b LSP) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}

func (l LSPConfig) ResolvedEnv() []string {
	return resolveEnvs(l.Env)
}

func (m MCPConfig) ResolvedEnv() []string {
	return resolveEnvs(m.Env)
}

func (m MCPConfig) ResolvedHeaders() map[string]string {
	resolver := NewShellVariableResolver(env.New())
	for e, v := range m.Headers {
		var err error
		m.Headers[e], err = resolver.ResolveValue(v)
		if err != nil {
			slog.Error("Error resolving header variable", "error", err, "variable", e, "value", v)
			continue
		}
	}
	return m.Headers
}

// Agent defines an ADK agent's identity and configuration.
// The map key in Config.Agents is the agent ID.
type Agent struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Model       SelectedModelType `json:"model" jsonschema:"required,description=The model type to use for this agent,enum=large,enum=small,default=large"`
	Disabled    bool              `json:"disabled,omitempty"`
}

type Tools struct {
	Ls   ToolLs   `json:"ls,omitzero"`
	Grep ToolGrep `json:"grep,omitzero"`
}

// StationConfig configures a factory station backed by an agentrun process.
// Each station is an independent workspace with its own backend, model, and
// permission mode. The station name becomes the ADK tool name.
type StationConfig struct {
	// Backend selects the agent CLI: "claude" (default), "codex", "opencode", "opencode-acp".
	Backend string `json:"backend,omitempty"`
	// Model is the model ID passed to the agent CLI (e.g. "claude-opus-4-20250514").
	Model string `json:"model,omitempty"`
	// Description is the tool description that the orchestrator LLM sees.
	Description string `json:"description,omitempty"`
	// Options are key-value pairs passed to the agentrun session.
	Options map[string]string `json:"options,omitempty"`
	// Disabled prevents the station tool from being registered.
	Disabled bool `json:"disabled,omitempty"`
	// Skill is the skill identifier to activate for this station.
	// Format: "<namespace>:<skill-name>" (e.g., "feature-dev:feature-dev").
	// When set, the first turn is prefixed with a skill-load instruction.
	Skill string `json:"skill,omitempty"`
	// Steering is an ephemeral reminder injected into the supervisor's system
	// instruction for one model call after this station's tool returns.
	// Users can override this per station in custom workflows (#12).
	Steering string `json:"steering,omitempty"`
	// ArtifactType is the artifact suffix for station results (e.g. "spec",
	// "patch", "verdict"). Defaults to "result" if empty.
	ArtifactType string `json:"artifact_type,omitempty"`
	// Gate requires operator approval before each station invocation.
	// "Allow for Session" ungates for the remainder of the session.
	Gate bool `json:"gate,omitempty"`
	// Env holds additional environment variables for the station's agent process.
	// These are merged with the parent process environment by agentrun.MergeEnv —
	// os.Environ() provides the base, and Env entries override matching keys.
	Env map[string]string `json:"env,omitempty"`
}

type ToolLs struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

// Limits returns the user-defined max-depth and max-items, or their defaults.
func (t ToolLs) Limits() (depth, items int) {
	return ptrValOr(t.MaxDepth, 0), ptrValOr(t.MaxItems, 0)
}

type ToolGrep struct {
	Timeout *time.Duration `json:"timeout,omitempty" jsonschema:"description=Timeout for the grep tool call,default=5s,example=10s"`
}

// GetTimeout returns the user-defined timeout or the default.
func (t ToolGrep) GetTimeout() time.Duration {
	return ptrValOr(t.Timeout, 5*time.Second)
}

// Config holds the configuration for crucible.
type Config struct {
	Schema string `json:"$schema,omitempty"`

	// We currently only support large/small as values here.
	Models map[SelectedModelType]SelectedModel `json:"models,omitempty" jsonschema:"description=Model configurations for different model types,example={\"large\":{\"model\":\"gpt-4o\",\"provider\":\"openai\"}}"`

	// Recently used models stored in the data directory config.
	RecentModels map[SelectedModelType][]SelectedModel `json:"recent_models,omitempty" jsonschema:"-"`

	// The providers that are configured
	Providers *csync.Map[string, ProviderConfig] `json:"providers,omitempty" jsonschema:"description=AI provider configurations"`

	MCP MCPs `json:"mcp,omitempty" jsonschema:"description=Model Context Protocol server configurations"`

	LSP LSPs `json:"lsp,omitempty" jsonschema:"description=Language Server Protocol configurations"`

	Options *Options `json:"options,omitempty" jsonschema:"description=General application options"`

	Permissions *Permissions `json:"permissions,omitempty" jsonschema:"description=Permission settings for tool usage"`

	Tools Tools `json:"tools,omitzero" jsonschema:"description=Tool configurations"`

	// Stations are factory workstations backed by agentrun processes.
	// Each station becomes an ADK tool available to the orchestrator LLM.
	Stations map[string]StationConfig `json:"stations,omitzero" jsonschema:"description=Factory station configurations"`

	Agents map[string]Agent `json:"-"`

	// Internal
	workingDir string `json:"-"`
	// TODO: find a better way to do this this should probably not be part of the config
	resolver       VariableResolver
	dataConfigDir  string             `json:"-"`
	knownProviders []ProviderMetadata `json:"-"`
}

func (c *Config) WorkingDir() string {
	return c.workingDir
}

// KnownProviders returns the provider catalog loaded at startup.
func (c *Config) KnownProviders() []ProviderMetadata {
	return c.knownProviders
}

func (c *Config) EnabledProviders() []ProviderConfig {
	var enabled []ProviderConfig
	for p := range c.Providers.Seq() {
		if !p.Disable {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// IsConfigured  return true if at least one provider is configured
func (c *Config) IsConfigured() bool {
	return len(c.EnabledProviders()) > 0
}

func (c *Config) GetModel(provider, model string) *ModelMetadata {
	if providerConfig, ok := c.Providers.Get(provider); ok {
		for _, m := range providerConfig.Models {
			if m.ID == model {
				return &m
			}
		}
	}
	return nil
}

func (c *Config) GetProviderForModel(modelType SelectedModelType) *ProviderConfig {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	if providerConfig, ok := c.Providers.Get(model.Provider); ok {
		return &providerConfig
	}
	return nil
}

func (c *Config) GetModelByType(modelType SelectedModelType) *ModelMetadata {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

func (c *Config) LargeModel() *ModelMetadata {
	model, ok := c.Models[SelectedModelTypeLarge]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

func (c *Config) SmallModel() *ModelMetadata {
	model, ok := c.Models[SelectedModelTypeSmall]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

// validThemeIDs is the canonical set of built-in theme identifiers.
// Kept in sync with the jsonschema enum on TUIOptions.Theme and
// styles.BuiltinThemeIDs(). Config can't import styles (cycle), so
// the list is duplicated here. Tests verify they stay in sync.
var validThemeIDs = map[string]bool{
	"steel-blue":     true,
	"amber-forge":    true,
	"phosphor-green": true,
	"reactor-red":    true,
	"titanium":       true,
	"clean-room":     true,
}

// validSpinnerPresets is the canonical set of spinner preset identifiers.
// Kept in sync with the jsonschema enum on TUIOptions.Spinner and
// anim.Presets(). Config can't import anim (cycle), so the list is
// duplicated here.
var validSpinnerPresets = map[string]bool{
	"industrial": true,
	"pulse":      true,
	"dots":       true,
	"ellipsis":   true,
	"points":     true,
	"meter":      true,
	"hamburger":  true,
	"trigram":    true,
}

func (c *Config) SetTheme(theme string) error {
	if !validThemeIDs[theme] {
		return fmt.Errorf("unknown theme %q", theme)
	}
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	c.Options.TUI.Theme = theme
	return c.SetConfigField("options.tui.theme", theme)
}

// ThemeID returns the configured theme or "steel-blue" as default.
func (c *Config) ThemeID() string {
	if c.Options != nil && c.Options.TUI != nil && c.Options.TUI.Theme != "" {
		return c.Options.TUI.Theme
	}
	return "steel-blue"
}

// SpinnerPreset returns the configured spinner preset or "meter" as default.
func (c *Config) SpinnerPreset() string {
	if c.Options != nil && c.Options.TUI != nil && c.Options.TUI.Spinner != "" {
		return c.Options.TUI.Spinner
	}
	return "meter"
}

// SetSpinner validates and persists the spinner preset.
func (c *Config) SetSpinner(preset string) error {
	if !validSpinnerPresets[preset] {
		return fmt.Errorf("unknown spinner preset %q", preset)
	}
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	c.Options.TUI.Spinner = preset
	return c.SetConfigField("options.tui.spinner", preset)
}

// IsTransparent returns whether transparent mode is enabled.
func (c *Config) IsTransparent() bool {
	return c.Options != nil && c.Options.TUI != nil &&
		c.Options.TUI.Transparent != nil && *c.Options.TUI.Transparent
}

// SetTransparent validates and persists the transparent mode setting.
func (c *Config) SetTransparent(transparent bool) error {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	c.Options.TUI.Transparent = &transparent
	return c.SetConfigField("options.tui.transparent", transparent)
}

func (c *Config) SetCompactMode(enabled bool) error {
	if c.Options == nil {
		c.Options = &Options{}
	}
	c.Options.TUI.CompactMode = enabled
	return c.SetConfigField("options.tui.compact_mode", enabled)
}

func (c *Config) Resolve(key string) (string, error) {
	if c.resolver == nil {
		return "", fmt.Errorf("no variable resolver configured")
	}
	return c.resolver.ResolveValue(key)
}

func (c *Config) UpdatePreferredModel(modelType SelectedModelType, model SelectedModel) error {
	c.Models[modelType] = model
	if err := c.SetConfigField(fmt.Sprintf("models.%s", modelType), model); err != nil {
		return fmt.Errorf("failed to update preferred model: %w", err)
	}
	if err := c.recordRecentModel(modelType, model); err != nil {
		return err
	}
	return nil
}

func (c *Config) HasConfigField(key string) bool {
	data, err := os.ReadFile(c.dataConfigDir)
	if err != nil {
		return false
	}
	return gjson.Get(string(data), key).Exists()
}

func (c *Config) SetConfigField(key string, value any) error {
	data, err := os.ReadFile(c.dataConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}

	newValue, err := sjson.Set(string(data), key, value)
	if err != nil {
		return fmt.Errorf("failed to set config field %s: %w", key, err)
	}
	if err := os.MkdirAll(filepath.Dir(c.dataConfigDir), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", c.dataConfigDir, err)
	}
	if err := os.WriteFile(c.dataConfigDir, []byte(newValue), 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func (c *Config) RemoveConfigField(key string) error {
	data, err := os.ReadFile(c.dataConfigDir)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	newValue, err := sjson.Delete(string(data), key)
	if err != nil {
		return fmt.Errorf("failed to delete config field %s: %w", key, err)
	}
	if err := os.MkdirAll(filepath.Dir(c.dataConfigDir), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", c.dataConfigDir, err)
	}
	if err := os.WriteFile(c.dataConfigDir, []byte(newValue), 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// RefreshOAuthToken refreshes the OAuth token for the given provider.
// Currently no providers support OAuth refresh (Gemini uses API keys).
func (c *Config) RefreshOAuthToken(_ context.Context, providerID string) error {
	return fmt.Errorf("OAuth refresh not supported for provider %s", providerID)
}

func (c *Config) SetProviderAPIKey(providerID string, apiKey any) error {
	var providerConfig ProviderConfig
	var exists bool
	var setKeyOrToken func()

	switch v := apiKey.(type) {
	case string:
		if err := c.SetConfigField(fmt.Sprintf("providers.%s.api_key", providerID), v); err != nil {
			return fmt.Errorf("failed to save api key to config file: %w", err)
		}
		setKeyOrToken = func() { providerConfig.APIKey = v }
	case *oauth.Token:
		if err := cmp.Or(
			c.SetConfigField(fmt.Sprintf("providers.%s.api_key", providerID), v.AccessToken),
			c.SetConfigField(fmt.Sprintf("providers.%s.oauth", providerID), v),
		); err != nil {
			return err
		}
		setKeyOrToken = func() {
			providerConfig.APIKey = v.AccessToken
			providerConfig.OAuthToken = v
		}
	}

	providerConfig, exists = c.Providers.Get(providerID)
	if exists {
		setKeyOrToken()
		c.Providers.Set(providerID, providerConfig)
		return nil
	}

	var foundProvider *ProviderMetadata
	for _, p := range c.knownProviders {
		if p.ID == providerID {
			foundProvider = &p
			break
		}
	}

	if foundProvider != nil {
		providerConfig = ProviderConfig{
			ID:           providerID,
			Name:         foundProvider.Name,
			BaseURL:      foundProvider.APIEndpoint,
			Type:         foundProvider.Type,
			Disable:      false,
			ExtraHeaders: make(map[string]string),
			ExtraParams:  make(map[string]string),
			Models:       foundProvider.Models,
		}
		setKeyOrToken()
	} else {
		return fmt.Errorf("provider with ID %s not found in known providers", providerID)
	}
	c.Providers.Set(providerID, providerConfig)
	return nil
}

const maxRecentModelsPerType = 5

func (c *Config) recordRecentModel(modelType SelectedModelType, model SelectedModel) error {
	if model.Provider == "" || model.Model == "" {
		return nil
	}

	if c.RecentModels == nil {
		c.RecentModels = make(map[SelectedModelType][]SelectedModel)
	}

	eq := func(a, b SelectedModel) bool {
		return a.Provider == b.Provider && a.Model == b.Model
	}

	entry := SelectedModel{
		Provider: model.Provider,
		Model:    model.Model,
	}

	current := c.RecentModels[modelType]
	withoutCurrent := slices.DeleteFunc(slices.Clone(current), func(existing SelectedModel) bool {
		return eq(existing, entry)
	})

	updated := append([]SelectedModel{entry}, withoutCurrent...)
	if len(updated) > maxRecentModelsPerType {
		updated = updated[:maxRecentModelsPerType]
	}

	if slices.EqualFunc(current, updated, eq) {
		return nil
	}

	c.RecentModels[modelType] = updated

	if err := c.SetConfigField(fmt.Sprintf("recent_models.%s", modelType), updated); err != nil {
		return fmt.Errorf("failed to persist recent models: %w", err)
	}

	return nil
}

// SetupAgents initializes the agent registry. Agent definitions here are the
// source of truth — they feed into ADK agent construction and are accessible
// by the UI and system prompt template.
func (c *Config) SetupAgents() {
	c.Agents = map[string]Agent{
		AgentCrucible: {
			Name:        "Crucible",
			Description: "Industrial software production system",
			Model:       SelectedModelTypeLarge,
		},
	}
}

// DefaultStations are the factory stations registered when no user config exists.
var DefaultStations = map[string]StationConfig{
	// Soft read-only (accepted risk): design has no Options{"mode":"plan"} because
	// plan mode triggers a lower-quality explorer model in Claude Code. The
	// claude-foundry:design skill enforces read-only behavior via prompt instead.
	// This is an intentional trade-off: better model quality at the cost of a
	// prompt-enforced (not system-enforced) read-only boundary.
	//
	// Steering assumes draft is enabled. If draft is disabled by user config,
	// design's steering routes to a missing station. Users who disable draft
	// should also customize design's steering or disable design.
	"design": {
		Backend:      "claude",
		Description:  "Send work to the design station for architectural analysis and solution design. The station has read-only access to the project workspace. Use for tasks involving architectural decisions, multiple valid approaches, cross-cutting concerns, or high blast-radius changes. The station explores the solution space, evaluates trade-offs, and produces a design document with decisions and rationale. It saves the design document to a file and reports the path — extract this path and pass it to draft for implementation planning. The station retains context across calls.",
		Skill:        "claude-foundry:design",
		ArtifactType: "design",
		Steering:     "Design station returned a design document. Extract the design file path from the result. Send the design to draft — include the design file path so draft can read it and produce an implementation plan aligned with the design decisions. Do not skip drafting.",
	},
	"draft": {
		Backend:      "claude",
		Description:  "Send work to the drafting station — produces technical documents, plans, specifications, and analyses. The station has read-only access to the project workspace. It can analyze code, explore files, and reason about architecture, but cannot modify anything. The station saves plans to files and reports the file path in its response — extract this path and pass it to downstream stations (inspect, build). Do NOT suggest filenames; the station chooses its own. The station retains context across calls within this session.",
		ArtifactType: "spec",
		Options: map[string]string{
			"mode": "plan",
		},
		Steering: "Draft station returned a plan. Extract the plan file path from the result. " +
			"Send the plan to inspect for verification before dispatching to build. Do not skip inspection.",
	},
	"inspect": {
		Backend:      "opencode-acp",
		Description:  "Send work to the inspection station for quality control. It has read-only access to the workspace. Primary use: validate plans and specifications before they reach build — include the plan file path so it can read and evaluate directly. Secondary use: quick validation of code output as a reinforcement check. The station retains context across calls.",
		Skill:        "review-plan",
		ArtifactType: "report",
		Steering: "Inspect station returned its verdict. If it passed — dispatch to build with the plan file path. " +
			"If it found critical issues — send findings back to draft to revise. Do not forward a rejected plan to build.",
	},
	"build": {
		Backend:      "claude",
		Description:  "Send implementation work to the build station — it has full read-write access to the project workspace. It can create files, edit code, run commands, and execute tests. Use for implementing approved plans, applying patches, and running build/test cycles. The station retains context across calls within this session.",
		Skill:        "feature-dev:feature-dev",
		ArtifactType: "patch",
		Steering: "Build station completed. If the task warrants quality validation, send to review or verify. " +
			"Check the result for errors or partial completion — re-dispatch to build if needed.",
	},
	"review": {
		Backend:      "claude",
		Description:  "Send code to the review station for structured code review. It has read-only access to the workspace. It reviews changes for correctness, style, conventions, and potential bugs, producing a pass/fail verdict with specific issues. Use after the build station completes implementation work.",
		Skill:        "claude-code-quality:rigorous-pr-review",
		ArtifactType: "verdict",
		Options: map[string]string{
			"mode": "plan",
		},
		Steering: "Review station returned its verdict. If it passed — dispatch to the next station in the workflow (verify or ship if available), or report completion to the user. " +
			"If it found issues — dispatch fixes back to build with specific issues listed, then re-review.",
	},
	"verify": {
		Backend:      "claude",
		Description:  "Send work to the verify station for execution-based validation. It has full read-write access to the project workspace. It runs tests, executes commands, checks logs, and verifies behavior end-to-end. This is NOT a code review — it runs the code and confirms it works. Use after build completes implementation. The station can fix trivial issues it discovers (missing imports, config typos) but should report larger problems back. The station retains context across calls.",
		ArtifactType: "verification",
		Steering: "Verify station returned its verdict. If it passed — dispatch to ship with a summary of what was built and the issue reference to close (e.g., \"Closes #N\"). " +
			"If ship is unavailable, report completion to the user. " +
			"If it found failures — dispatch the failure details back to build for fixes. After build fixes, re-dispatch to verify.",
	},
	"ship": {
		Backend:      "claude",
		Description:  "Send work to the ship station to package verified changes into a pull request. It has full read-write access to the project workspace. It stages files, writes meaningful commit messages, pushes to a feature branch, and creates a PR via `gh pr create` with a description linking the originating issue. The PR is the deliverable — it does NOT merge. Include the issue reference and a summary of what was built in the task. Requires operator approval before execution (gated). The station retains context across calls.",
		ArtifactType: "pr",
		Gate:         true,
		Steering: "Ship station returned its result. If it created a PR — report the PR URL to the user and confirm pipeline completion. " +
			"If it failed — review the error (auth, conflicts, permissions) and report to the user with actionable next steps.",
	},
}

// SetupDefaultStations is a safety net that ensures the stations map exists and
// adds any default stations that are completely absent. Field-level merging of
// user overrides with defaults is handled upstream by stationDefaultsJSON() +
// jsons.Merge in loadFromConfigPaths().
func (c *Config) SetupDefaultStations() {
	if c.Stations == nil {
		c.Stations = make(map[string]StationConfig, len(DefaultStations))
	}
	for name, def := range DefaultStations {
		if _, exists := c.Stations[name]; !exists {
			c.Stations[name] = def
		}
	}
}

func (c *Config) Resolver() VariableResolver {
	return c.resolver
}

func (c *ProviderConfig) TestConnection(resolver VariableResolver) error {
	var (
		testURL   = ""
		headers   = make(map[string]string)
		apiKey, _ = resolver.ResolveValue(c.APIKey)
	)

	switch c.Type {
	case ProviderTypeOpenAI, ProviderTypeOpenAICompat:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.openai.com/v1")
		testURL = baseURL + "/models"
		headers["Authorization"] = "Bearer " + apiKey
	case ProviderTypeAnthropic:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.anthropic.com/v1")
		testURL = baseURL + "/models"
		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	case ProviderTypeGemini:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://generativelanguage.googleapis.com")
		testURL = baseURL + "/v1beta/models"
		headers["x-goog-api-key"] = apiKey
	}

	if testURL == "" {
		return fmt.Errorf("no test endpoint for provider type %s", c.Type)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for provider %s: %w", c.ID, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range c.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to provider %s: %w", c.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to connect to provider %s: %s", c.ID, resp.Status)
	}
	return nil
}

func resolveEnvs(envs map[string]string) []string {
	resolver := NewShellVariableResolver(env.New())
	for e, v := range envs {
		var err error
		envs[e], err = resolver.ResolveValue(v)
		if err != nil {
			slog.Error("Error resolving environment variable", "error", err, "variable", e, "value", v)
			continue
		}
	}

	res := make([]string, 0, len(envs))
	for k, v := range envs {
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res
}

func ptrValOr[T any](t *T, el T) T {
	if t == nil {
		return el
	}
	return *t
}
