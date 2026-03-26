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
	// CredentialsFile is the path to a Google service account JSON key file.
	// When set, Vertex AI uses this file instead of ADC.
	CredentialsFile string `json:"credentials_file,omitempty" jsonschema:"description=Path to service account JSON key file for Vertex AI"`
	// Credential is the canonical credential for this provider (new unified type).
	Credential Credential `json:"credential,omitempty"`

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
	// Requires lists station names that must have completed (VerdictDone) in
	// the session's dispatch log before this station can run.
	Requires []string `json:"requires,omitempty"`
	// AfterDone lists station names that must have completed (VerdictDone)
	// more recently than this station's last VerdictDone before this station
	// can dispatch again. Failed/canceled dispatches do not trigger this
	// constraint, allowing retries.
	AfterDone []string `json:"after_done,omitempty"`
	// RequiresArtifact lists station names whose active artifact must exist
	// in session state before this station can dispatch. Checked after route
	// enforcement, before gate. Empty = no artifact requirement.
	RequiresArtifact []string `json:"requires_artifact,omitempty"`
	// DisableArtifactEnforcement overrides RequiresArtifact when true.
	// Exists because jsons.Merge appends arrays — setting requires_artifact:[]
	// in user config does NOT clear the default. This boolean provides a
	// non-additive escape hatch.
	DisableArtifactEnforcement bool `json:"disable_artifact_enforcement,omitempty"`
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
	loadedPaths    []string           `json:"-"` // config paths in merge order (lowest→highest priority)
	knownProviders []ProviderMetadata `json:"-"`
}

func (c *Config) WorkingDir() string {
	return c.workingDir
}

// LoadedPaths returns the config file paths in merge order (lowest→highest priority).
func (c *Config) LoadedPaths() []string {
	return c.loadedPaths
}

// ConfigScope identifies which config layer holds an override.
type ConfigScope string

const (
	ConfigScopeDefault ConfigScope = "default" // only in DefaultStations, no file override
	ConfigScopeUser    ConfigScope = "user"    // ~/.config/crucible/crucible.json
	ConfigScopeGlobal  ConfigScope = "global"  // ~/.local/share/crucible/crucible.json
	ConfigScopeProject ConfigScope = "project" // project-level crucible.json
)

// classifyPath determines which scope a config file path belongs to.
func (c *Config) classifyPath(path string) ConfigScope {
	if path == GlobalConfig() {
		return ConfigScopeUser
	}
	if path == GlobalConfigData() || path == c.dataConfigDir {
		return ConfigScopeGlobal
	}
	return ConfigScopeProject
}

// ProjectConfigPath returns the most specific project-level config path.
// Falls back to {workingDir}/crucible.json if no project config was loaded.
func (c *Config) ProjectConfigPath() string {
	for i := len(c.loadedPaths) - 1; i >= 0; i-- {
		if c.classifyPath(c.loadedPaths[i]) == ConfigScopeProject {
			return c.loadedPaths[i]
		}
	}
	if c.workingDir != "" {
		return filepath.Join(c.workingDir, appName+".json")
	}
	return ""
}

// StationScope determines which config file layer holds the override for a station.
func (c *Config) StationScope(stationName string) ConfigScope {
	key := "stations." + stationName
	for i := len(c.loadedPaths) - 1; i >= 0; i-- {
		path := c.loadedPaths[i]
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if gjson.Get(string(data), key).Exists() {
			return c.classifyPath(path)
		}
	}
	return ConfigScopeDefault
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
	return writeConfigField(c.dataConfigDir, key, value)
}

func (c *Config) RemoveConfigField(key string) error {
	return removeConfigField(c.dataConfigDir, key)
}

// SetScopedConfigField writes a config field to the file for the given scope.
func (c *Config) SetScopedConfigField(key string, value any, scope ConfigScope) error {
	path := c.writePathForScope(scope)
	if path == "" {
		return fmt.Errorf("no writable config path for scope %q", scope)
	}
	// Register newly created config path so scope lookups (e.g. StationScope)
	// can find it without requiring a full config reload.
	if !slices.Contains(c.loadedPaths, path) {
		c.loadedPaths = append(c.loadedPaths, path)
	}
	return writeConfigField(path, key, value)
}

// RemoveScopedConfigField removes a config field from the file for the given scope.
func (c *Config) RemoveScopedConfigField(key string, scope ConfigScope) error {
	path := c.writePathForScope(scope)
	if path == "" {
		return fmt.Errorf("no writable config path for scope %q", scope)
	}
	return removeConfigField(path, key)
}

func (c *Config) writePathForScope(scope ConfigScope) string {
	switch scope {
	case ConfigScopeProject:
		return c.ProjectConfigPath()
	case ConfigScopeGlobal:
		return c.dataConfigDir
	case ConfigScopeUser:
		return GlobalConfig()
	default:
		return "" // default scope is not writable
	}
}

// writeConfigField is the shared read-modify-write helper for config file updates.
func writeConfigField(path, key string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return fmt.Errorf("failed to read config file %s: %w", path, err)
		}
	}
	newValue, err := sjson.Set(string(data), key, value)
	if err != nil {
		return fmt.Errorf("failed to set config field %s: %w", key, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	return os.WriteFile(path, []byte(newValue), 0o600)
}

// removeConfigField is the shared read-modify-write helper for config field removal.
func removeConfigField(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	newValue, err := sjson.Delete(string(data), key)
	if err != nil {
		return fmt.Errorf("failed to delete config field %s: %w", key, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	return os.WriteFile(path, []byte(newValue), 0o600)
}

// ActiveCredential returns the canonical Credential for this provider.
// If the new Credential field is set, it is authoritative. Otherwise, infers
// from legacy fields for backward compatibility.
func (p ProviderConfig) ActiveCredential() Credential {
	if p.Credential != (Credential{}) {
		return p.Credential
	}
	if p.OAuthToken != nil && p.OAuthToken.AccessToken != "" {
		return Credential{Kind: CredentialOAuth, OAuthToken: p.OAuthToken}
	}
	if p.APIKey != "" {
		return Credential{Kind: CredentialAPIKey, APIKey: p.APIKey}
	}
	if p.CredentialsFile != "" {
		return Credential{Kind: CredentialServiceAccount, CredentialsFile: p.CredentialsFile}
	}
	return Credential{Kind: CredentialADC}
}

// RefreshOAuthToken refreshes the OAuth token for the given provider.
// Currently no providers support OAuth refresh (Gemini uses API keys).
func (c *Config) RefreshOAuthToken(_ context.Context, providerID string) error {
	return fmt.Errorf("OAuth refresh not supported for provider %s", providerID)
}

// SetProviderCredential atomically sets the credential for a provider,
// persists it to disk, and clears stale legacy fields.
func (c *Config) SetProviderCredential(providerID string, cred Credential) error {
	prov, ok := c.Providers.Get(providerID)
	if !ok {
		var found *ProviderMetadata
		for _, p := range c.knownProviders {
			if p.ID == providerID {
				found = &p
				break
			}
		}
		if found == nil {
			return fmt.Errorf("provider %q not found in known providers", providerID)
		}
		prov = ProviderConfig{
			ID:           providerID,
			Name:         found.Name,
			BaseURL:      found.APIEndpoint,
			Type:         found.Type,
			ExtraHeaders: make(map[string]string),
			ExtraParams:  make(map[string]string),
			Models:       found.Models,
		}
	}
	prov.Credential = cred
	prov.Backend = cred.Backend()
	prov.APIKey = ""
	prov.OAuthToken = nil
	prov.CredentialsFile = ""
	switch cred.Kind {
	case CredentialAPIKey:
		prov.APIKey = cred.APIKey
	case CredentialOAuth:
		prov.OAuthToken = cred.OAuthToken
	case CredentialServiceAccount:
		prov.CredentialsFile = cred.CredentialsFile
	}
	prefix := fmt.Sprintf("providers.%s", providerID)
	if err := c.persistProviderFields(prefix, &prov); err != nil {
		return err
	}
	c.Providers.Set(providerID, prov)
	return nil
}

// UpdateProvider atomically reads, mutates, persists, and sets a provider config.
// The mutate function receives a pointer to the copy — changes are applied only if
// mutate returns nil.
func (c *Config) UpdateProvider(providerID string, mutate func(*ProviderConfig) error) error {
	prov, ok := c.Providers.Get(providerID)
	if !ok {
		return fmt.Errorf("provider %q not found", providerID)
	}

	if err := mutate(&prov); err != nil {
		return err
	}

	prefix := fmt.Sprintf("providers.%s", providerID)
	if err := c.persistProviderFields(prefix, &prov); err != nil {
		return err
	}

	c.Providers.Set(providerID, prov)
	return nil
}

func (c *Config) persistProviderFields(prefix string, prov *ProviderConfig) error {
	if err := c.SetConfigField(prefix+".backend", prov.Backend); err != nil {
		return err
	}
	for _, kv := range []struct {
		val string
		key string
	}{
		{prov.CredentialsFile, prefix + ".credentials_file"},
		{prov.APIKey, prefix + ".api_key"},
		{prov.Project, prefix + ".project"},
		{prov.Location, prefix + ".location"},
	} {
		if kv.val != "" {
			if err := c.SetConfigField(kv.key, kv.val); err != nil {
				return err
			}
		}
	}
	if prov.OAuthToken != nil {
		if err := c.SetConfigField(prefix+".oauth", prov.OAuthToken); err != nil {
			return err
		}
	}
	if prov.Credential != (Credential{}) {
		if err := c.SetConfigField(prefix+".credential", prov.Credential); err != nil {
			return err
		}
	}
	return c.removeEmptyProviderFields(prefix, prov)
}

func (c *Config) removeEmptyProviderFields(prefix string, prov *ProviderConfig) error {
	for _, kv := range []struct {
		empty bool
		key   string
	}{
		{prov.APIKey == "", prefix + ".api_key"},
		{prov.OAuthToken == nil, prefix + ".oauth"},
		{prov.CredentialsFile == "", prefix + ".credentials_file"},
		{prov.Credential == (Credential{}), prefix + ".credential"},
	} {
		if kv.empty {
			if err := c.RemoveConfigField(kv.key); err != nil {
				return err
			}
		}
	}
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

// DefaultStations defines the tools registered when no user config exists.
var DefaultStations = map[string]StationConfig{
	// Soft read-only (accepted risk): design has no Options{"mode":"plan"} because
	// plan mode triggers a lower-quality explorer model in Claude Code. The
	// claude-foundry:design skill enforces read-only behavior via prompt instead.
	//
	// Steering assumes plan is enabled. If plan is disabled by user config,
	// design's steering routes to a missing tool. Users who disable plan
	// should also customize design's steering or disable design.
	"design": {
		Backend:      "claude",
		Description:  "Analyze architecture, evaluate trade-offs, and produce a design document. Saves the document to a file and returns the path. Retains context across calls.",
		Skill:        "claude-foundry:design",
		ArtifactType: "design",
		Steering:     "Design produced a document. Extract the file path and pass it to plan.",
	},
	"plan": {
		Backend:      "claude",
		Description:  "Produce technical plans, specifications, and analyses. Saves plans to files and returns the file path. Do not suggest filenames. Retains context across calls.",
		ArtifactType: "spec",
		Options: map[string]string{
			"mode": "plan",
		},
		Steering: "Plan produced a specification. The artifact_path field contains the file path. " +
			"Pass this path to inspect before build.",
	},
	"inspect": {
		Backend:          "opencode-acp",
		Description:      "Validate plans and specifications before implementation. Include the plan file path so it can read and evaluate directly. Returns a pass/fail verdict with specific issues. Retains context across calls.",
		Skill:            "review-plan",
		ArtifactType:     "report",
		RequiresArtifact: []string{"plan"},
		Steering: "Inspect verdict received. If passed — dispatch build (the plan artifact is auto-injected). " +
			"If critical issues — dispatch plan to revise.",
	},
	"build": {
		Backend:          "claude",
		Description:      "Implement code changes — create files, edit code, run commands, execute tests. Retains context across calls.",
		Skill:            "feature-dev:feature-dev",
		ArtifactType:     "patch",
		Requires:         []string{"plan"},
		RequiresArtifact: []string{"plan"},
		Steering:         "Build completed. Check result for errors. If quality validation needed — use review or verify.",
	},
	"review": {
		Backend:      "claude",
		Description:  "Structured code review for correctness, style, conventions, and potential bugs. Returns a pass/fail verdict with specific issues. Retains context across calls.",
		Skill:        "claude-code-quality:rigorous-pr-review",
		ArtifactType: "verdict",
		Requires:     []string{"build"},
		Options: map[string]string{
			"mode": "plan",
		},
		Steering: "Review verdict received. If passed — continue to verify or ship. " +
			"If issues found — use build to fix, then re-review.",
	},
	"verify": {
		Backend:      "claude",
		Description:  "Verify that changes actually work. Auto-detects what changed (frontend, API, CLI, TUI, tests), probes available verification tools (browser, tmux, test runners), runs appropriate checks, and reports a pass/partial/fail/manual verdict. Retains context across calls.",
		Skill:        "claude-foundry:verify",
		ArtifactType: "verification",
		Requires:     []string{"review"},
		AfterDone:    []string{"review"},
		Steering: "Verify verdict received. If passed — use ship with a summary and issue reference (e.g., \"Closes #N\"). " +
			"If failures — use build to fix, then re-verify.",
	},
	"ship": {
		Backend:      "claude",
		Description:  "Create a pull request — stage files, commit, push branch, open PR via `gh pr create` with a description linking the originating issue. The PR is the deliverable — it does not merge. Include the issue reference and a summary of what was built. Requires operator approval. Retains context across calls.",
		ArtifactType: "pr",
		Requires:     []string{"verify"},
		Gate:         true,
		Steering:     "Ship completed. If PR created — report the PR URL. If failed — report error with next steps.",
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
