package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"google.golang.org/adk/artifact"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/agent/prompt"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/oauth"
	"github.com/dmora/crucible/internal/permission"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
)

type Coordinator interface {
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*AgentResult, error)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	Model() Model
	UpdateModels(ctx context.Context) error
	TurnMetrics(sessionID string) *TurnMetrics
	AgentName() string
	StopProcess(ctx context.Context, sessionID string)
	StopAllProcesses(ctx context.Context)
	ProcessStates() map[string]ProcessInfo
	HydrateProcessStates(ctx context.Context, sessionID string) error
	ExecuteUserShell(ctx context.Context, sessionID, command string) error
	ReloadStations()
	SetHold()
	ClearHold()
	IsHoldActive() bool
	WorktreeInfo(sessionID string) *WorktreeInfoView
	SetWorktreeInfo(sessionID string, cwd, branch string)
	PurgeSession(sessionID string)

	// SkipArtifactCheck sets a session-level flag that bypasses artifact enforcement.
	SkipArtifactCheck(ctx context.Context, sessionID string) error

	// Relay mode: direct operator-to-station communication.
	StartRelay(ctx context.Context, sessionID, station string) error
	SendRelay(ctx context.Context, sessionID, message string) error
	StopRelay(ctx context.Context, sessionID string) error
	SwitchRelay(ctx context.Context, sessionID, newStation string) error
	CancelRelayTurn(sessionID string)
	RelayTarget(sessionID string) *string
	IsRelayActive(sessionID string) bool
	IsRelayTurnBusy(sessionID string) bool
}

// WorktreeInfoView is an exported view of worktree state for UI consumers.
type WorktreeInfoView struct {
	ResolvedCWD string
	Branch      string
}

type coordinator struct {
	cfg               *config.Config
	sessions          session.Service
	messageBroker     *pubsub.Broker[message.Message]
	adkSessionService adksession.Service
	artifactService   artifact.Service
	askUserService    askuser.Service
	permissionService permission.Service
	holdFlag          atomic.Bool

	currentAgent   SessionAgent
	currentAgentID string
	agents         map[string]SessionAgent

	worktreeManager *WorktreeManagerAdapter // nil when worktrees disabled
	worktreeInfos   *csync.Map[string, *WorktreeInfoView]

	readyWg errgroup.Group
}

// WorktreeManagerAdapter wraps worktree.Manager callbacks to avoid importing
// the worktree package in coordinator.go. Created by the app layer via
// NewWorktreeManagerAdapter.
type WorktreeManagerAdapter struct {
	ProvisionFn func(sessionID string) (cwd, branch string, err error)
	StatusFn    func(sessionID string) (cwd, branch string, ok bool)
}

// NewWorktreeManagerAdapter creates an adapter from a worktree.Manager-like interface.
func NewWorktreeManagerAdapter(provision func(string) (string, string, error), status func(string) (string, string, bool)) *WorktreeManagerAdapter {
	return &WorktreeManagerAdapter{
		ProvisionFn: provision,
		StatusFn:    status,
	}
}

func NewCoordinator(
	ctx context.Context,
	cfg *config.Config,
	sessions session.Service,
	messageBroker *pubsub.Broker[message.Message],
	adkSessionService adksession.Service,
	artifactService artifact.Service,
	askUserSvc askuser.Service,
	permissionSvc permission.Service,
) (Coordinator, error) {
	c := &coordinator{
		cfg:               cfg,
		sessions:          sessions,
		messageBroker:     messageBroker,
		adkSessionService: adkSessionService,
		artifactService:   artifactService,
		askUserService:    askUserSvc,
		permissionService: permissionSvc,
		agents:            make(map[string]SessionAgent),
		worktreeInfos:     csync.NewMap[string, *WorktreeInfoView](),
	}

	if _, ok := cfg.Agents[config.AgentCrucible]; !ok {
		return nil, errors.New("crucible agent not configured")
	}

	c.currentAgentID = config.AgentCrucible

	// TODO: make this dynamic when we support multiple agents
	p, err := coderPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, p)
	if err != nil {
		return nil, err
	}
	c.currentAgent = agent
	c.agents[config.AgentCrucible] = agent
	return c, nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*AgentResult, error) {
	if err := c.readyWg.Wait(); err != nil {
		return nil, err
	}

	// refresh models before each run
	if err := c.UpdateModels(ctx); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := c.currentAgent.Model()
	maxTokens := model.Metadata.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	if !model.Metadata.SupportsAttachments && attachments != nil {
		// filter out image attachments
		filteredAttachments := make([]message.Attachment, 0, len(attachments))
		for _, att := range attachments {
			if att.IsText() {
				filteredAttachments = append(filteredAttachments, att)
			}
		}
		attachments = filteredAttachments
	}

	// Provision worktree for this session if enabled and not already provisioned.
	if c.worktreeManager != nil {
		if _, ok := c.worktreeInfos.Get(sessionID); !ok {
			cwd, branch, err := c.worktreeManager.ProvisionFn(sessionID)
			if err != nil {
				return nil, fmt.Errorf("worktree provision: %w", err)
			}
			c.SetWorktreeInfo(sessionID, cwd, branch)
		}
	}

	return c.currentAgent.Run(ctx, SessionAgentCall{
		SessionID:       sessionID,
		Prompt:          prompt,
		Attachments:     attachments,
		MaxOutputTokens: maxTokens,
		Temperature:     model.ModelCfg.Temperature,
		TopP:            model.ModelCfg.TopP,
		TopK:            model.ModelCfg.TopK,
	})
}

func (c *coordinator) buildAgent(ctx context.Context, p *prompt.Prompt) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, ok := c.cfg.Providers.Get(large.ModelCfg.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", large.ModelCfg.Provider)
	}
	agentDef := c.cfg.Agents[c.currentAgentID]
	result := NewSessionAgent(SessionAgentOptions{
		AgentDef:           agentDef,
		Stations:           c.cfg.Stations,
		LargeModel:         large,
		SmallModel:         small,
		SystemPromptPrefix: largeProviderCfg.SystemPromptPrefix,
		Sessions:           c.sessions,
		MessageBroker:      c.messageBroker,
		ADKSessionService:  c.adkSessionService,
		ArtifactService:    c.artifactService,
		AskUserService:     c.askUserService,
		PermissionService:  c.permissionService,
		HoldFlag:           &c.holdFlag,
		WorkingDir:         c.cfg.WorkingDir(),
		Config:             c.cfg,
	})

	c.readyWg.Go(func() error {
		systemPrompt, err := p.Build(ctx, c.currentAgentID, large.ModelCfg.Provider, large.ModelCfg.Model, *c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	return result, nil
}

func (c *coordinator) buildAgentModels(ctx context.Context) (Model, Model, error) {
	largeModelCfg, ok := c.cfg.Models[config.SelectedModelTypeLarge]
	if !ok {
		return Model{}, Model{}, errors.New("large model not selected")
	}
	smallModelCfg, ok := c.cfg.Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, Model{}, errors.New("small model not selected")
	}

	largeProviderCfg, ok := c.cfg.Providers.Get(largeModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errors.New("large model provider not configured")
	}
	smallProviderCfg, ok := c.cfg.Providers.Get(smallModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errors.New("small model provider not configured")
	}

	// Build Gemini models via ADK (provider config carries backend + auth info).
	// API keys are already resolved by configureProviders(); no re-resolution needed.
	largeLLM, largeAuth, err := buildGeminiModel(ctx, largeModelCfg.Model, largeProviderCfg)
	if err != nil {
		return Model{}, Model{}, fmt.Errorf("build large model: %w", err)
	}
	smallLLM, smallAuth, err := buildGeminiModel(ctx, smallModelCfg.Model, smallProviderCfg)
	if err != nil {
		return Model{}, Model{}, fmt.Errorf("build small model: %w", err)
	}
	_ = smallAuth // small model auth is the same provider; UI reads from large model

	// Look up model metadata
	var largeMetadata *config.ModelMetadata
	for _, m := range largeProviderCfg.Models {
		if m.ID == largeModelCfg.Model {
			largeMetadata = &m
			break
		}
	}
	var smallMetadata *config.ModelMetadata
	for _, m := range smallProviderCfg.Models {
		if m.ID == smallModelCfg.Model {
			smallMetadata = &m
			break
		}
	}

	if largeMetadata == nil {
		return Model{}, Model{}, errors.New("large model not found in provider config")
	}
	if smallMetadata == nil {
		return Model{}, Model{}, errors.New("small model not found in provider config")
	}

	return Model{
			LLM:      largeLLM,
			Metadata: *largeMetadata,
			ModelCfg: largeModelCfg,
			Auth:     largeAuth,
		}, Model{
			LLM:      smallLLM,
			Metadata: *smallMetadata,
			ModelCfg: smallModelCfg,
			Auth:     largeAuth,
		}, nil
}

// buildGeminiModel creates an ADK LLM for a Gemini model using the provider config
// to determine the backend (Gemini API vs Vertex AI) and auth method.
func buildGeminiModel(ctx context.Context, modelName string, providerCfg config.ProviderConfig) (adkmodel.LLM, config.AuthInfo, error) {
	retryCfg := DefaultRetryTransportConfig()
	clientCfg, auth, err := resolveCredential(ctx, providerCfg, retryCfg)
	if err != nil {
		return nil, config.AuthInfo{}, fmt.Errorf("resolve credential: %w", err)
	}
	llm, err := gemini.NewModel(ctx, modelName, &clientCfg)
	if err != nil {
		return nil, config.AuthInfo{}, fmt.Errorf("gemini model %q: %w", modelName, err)
	}
	slog.Debug("Built Gemini model", "model", modelName, "backend", auth.Backend, "method", auth.Method)
	return llm, auth, nil
}

func resolveCredential(ctx context.Context, prov config.ProviderConfig, retryCfg RetryTransportConfig) (genai.ClientConfig, config.AuthInfo, error) {
	cred := prov.ActiveCredential()
	loc := cmp.Or(prov.Location, "us-central1")
	switch cred.Kind {
	case config.CredentialAPIKey:
		return genai.ClientConfig{
			APIKey: cred.APIKey, Backend: genai.BackendGeminiAPI,
			HTTPClient: &http.Client{Transport: NewRetryTransport(http.DefaultTransport, retryCfg)},
		}, config.AuthInfo{Backend: config.GeminiBackendAPI, Method: config.AuthMethodAPIKey, User: config.MaskAPIKey(cred.APIKey)}, nil

	case config.CredentialOAuth:
		if cred.OAuthToken == nil {
			return genai.ClientConfig{}, config.AuthInfo{}, errors.New("OAuth credential selected but no token found")
		}
		// Use a reusable token source that auto-refreshes via the refresh token.
		oauthConf := oauth.OAuthConfig()
		tok := &oauth2.Token{
			AccessToken:  cred.OAuthToken.AccessToken,
			RefreshToken: cred.OAuthToken.RefreshToken,
			Expiry:       time.Unix(cred.OAuthToken.ExpiresAt, 0),
		}
		ts := oauthConf.TokenSource(ctx, tok)
		return genai.ClientConfig{
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: &http.Client{Transport: NewRetryTransport(&oauth2.Transport{Source: ts, Base: http.DefaultTransport}, retryCfg)},
			HTTPOptions: genai.HTTPOptions{
				BaseURL:    "https://cloudcode-pa.googleapis.com",
				APIVersion: "v1internal",
			},
		}, config.AuthInfo{Backend: config.GeminiBackendAPI, Method: config.AuthMethodOAuth, User: "Google Account"}, nil

	case config.CredentialServiceAccount:
		data, err := os.ReadFile(cred.CredentialsFile) //nolint:gosec // user-controlled path is expected
		if err != nil {
			return genai.ClientConfig{}, config.AuthInfo{}, fmt.Errorf("read service account key: %w", err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, "https://www.googleapis.com/auth/cloud-platform") //nolint:staticcheck // SA1019: no replacement available in current SDK version
		if err != nil {
			return genai.ClientConfig{}, config.AuthInfo{}, fmt.Errorf("parse service account key: %w", err)
		}
		method, user := config.ReadCredentialsFile(cred.CredentialsFile)
		return genai.ClientConfig{
			Backend: genai.BackendVertexAI, Project: prov.Project, Location: loc,
			HTTPClient: &http.Client{Transport: NewRetryTransport(&oauth2.Transport{Source: creds.TokenSource, Base: http.DefaultTransport}, retryCfg)},
		}, config.AuthInfo{Backend: config.GeminiBackendVertex, Method: method, User: user, Project: prov.Project, Location: loc}, nil

	case config.CredentialADC:
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return genai.ClientConfig{}, config.AuthInfo{}, fmt.Errorf("ADC: %w", err)
		}
		method, user := config.DetectVertexAuth()
		return genai.ClientConfig{
			Backend: genai.BackendVertexAI, Project: prov.Project, Location: loc,
			HTTPClient: &http.Client{Transport: NewRetryTransport(&oauth2.Transport{Source: ts, Base: http.DefaultTransport}, retryCfg)},
		}, config.AuthInfo{Backend: config.GeminiBackendVertex, Method: method, User: user, Project: prov.Project, Location: loc}, nil

	default:
		return genai.ClientConfig{}, config.AuthInfo{}, fmt.Errorf("unknown credential kind: %d", cred.Kind)
	}
}

func (c *coordinator) ExecuteUserShell(ctx context.Context, sessionID, command string) error {
	return c.currentAgent.ExecuteUserShell(ctx, sessionID, command)
}

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
}

func (c *coordinator) AgentName() string {
	if a, ok := c.cfg.Agents[c.currentAgentID]; ok {
		return a.Name
	}
	return c.currentAgentID
}

func (c *coordinator) Model() Model {
	return c.currentAgent.Model()
}

func (c *coordinator) TurnMetrics(sessionID string) *TurnMetrics {
	return c.currentAgent.TurnMetrics(sessionID)
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	large, small, err := c.buildAgentModels(ctx)
	if err != nil {
		return err
	}
	c.currentAgent.SetModels(large, small)
	return nil
}

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	return c.currentAgent.Summarize(ctx, sessionID)
}

func (c *coordinator) StopProcess(ctx context.Context, sessionID string) {
	c.currentAgent.StopProcess(ctx, sessionID)
}

func (c *coordinator) StopAllProcesses(ctx context.Context) {
	c.currentAgent.StopAllProcesses(ctx)
}

func (c *coordinator) ProcessStates() map[string]ProcessInfo {
	return GetProcessStates()
}

func (c *coordinator) ReloadStations() {
	c.currentAgent.ReloadStations(c.cfg, c.cfg.Stations)
}

func (c *coordinator) SetHold() {
	c.holdFlag.Store(true)
}

func (c *coordinator) ClearHold() {
	c.holdFlag.Store(false)
}

func (c *coordinator) IsHoldActive() bool {
	return c.holdFlag.Load()
}

// WorktreeInfo returns the worktree info for a session, or nil if not set.
func (c *coordinator) WorktreeInfo(sessionID string) *WorktreeInfoView {
	if v, ok := c.worktreeInfos.Get(sessionID); ok {
		return v
	}
	return nil
}

// SetWorktreeInfo stores worktree info for a session (used for boot-time hydration).
func (c *coordinator) SetWorktreeInfo(sessionID string, cwd, branch string) {
	c.worktreeInfos.Set(sessionID, &WorktreeInfoView{
		ResolvedCWD: cwd,
		Branch:      branch,
	})
	// Also set on the current agent so station CWDs and per-turn injection work.
	if c.currentAgent != nil {
		c.currentAgent.SetSessionWorktree(sessionID, cwd, branch)
	}
}

// --- Relay delegation ---

func (c *coordinator) StartRelay(ctx context.Context, sessionID, station string) error {
	return c.currentAgent.StartRelay(ctx, sessionID, station)
}

func (c *coordinator) SendRelay(ctx context.Context, sessionID, msg string) error {
	return c.currentAgent.SendRelay(ctx, sessionID, msg)
}

func (c *coordinator) StopRelay(ctx context.Context, sessionID string) error {
	return c.currentAgent.StopRelay(ctx, sessionID)
}

func (c *coordinator) SwitchRelay(ctx context.Context, sessionID, newStation string) error {
	return c.currentAgent.SwitchRelay(ctx, sessionID, newStation)
}

func (c *coordinator) CancelRelayTurn(sessionID string) {
	c.currentAgent.CancelRelayTurn(sessionID)
}

func (c *coordinator) RelayTarget(sessionID string) *string {
	return c.currentAgent.RelayTarget(sessionID)
}

func (c *coordinator) IsRelayActive(sessionID string) bool {
	return c.currentAgent.IsRelayActive(sessionID)
}

func (c *coordinator) IsRelayTurnBusy(sessionID string) bool {
	return c.currentAgent.IsRelayTurnBusy(sessionID)
}

// PurgeSession removes all per-session worktree state from the coordinator and agent.
func (c *coordinator) PurgeSession(sessionID string) {
	c.worktreeInfos.Del(sessionID)
	if c.currentAgent != nil {
		c.currentAgent.PurgeSession(sessionID)
	}
}

func (c *coordinator) SkipArtifactCheck(ctx context.Context, sessionID string) error {
	return c.currentAgent.SkipArtifactCheck(ctx, sessionID)
}

// SetWorktreeManager configures worktree provisioning for the coordinator.
func (c *coordinator) SetWorktreeManager(mgr *WorktreeManagerAdapter) {
	c.worktreeManager = mgr
}

func (c *coordinator) HydrateProcessStates(ctx context.Context, sessionID string) error {
	resp, err := c.adkSessionService.Get(ctx, &adksession.GetRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err != nil {
		if isSessionNotFoundError(err) {
			return nil // no ADK session yet — no-op
		}
		return fmt.Errorf("hydrate process states: %w", err)
	}
	return HydrateSessionProcessStates(resp.Session, sessionID, c.cfg.Stations)
}
