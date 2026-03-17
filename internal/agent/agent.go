// Package agent is the core orchestration layer for Crucible AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations and message handling. It coordinates interactions between
// language models, messages, sessions while handling features like automatic
// summarization, queuing, and token management.
//
// Uses Google ADK (Agent Development Kit) as the LLM engine.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/artifact"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/adk/tool/loadartifactstool"
	"google.golang.org/genai"

	"github.com/dmora/adk-go-extras/plugin/notify"
	"github.com/dmora/crucible/internal/agent/tools/mcp"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/permission"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
	"github.com/google/uuid"
)

const (
	// DefaultSessionName is the placeholder title before async title generation.
	// Exported so the UI can detect untitled state without magic string checks.
	DefaultSessionName = "Untitled Session"

	// adkAppName is the app name used for ADK session persistence.
	// Changing this would break existing session lookups.
	adkAppName = "crucible"
	// adkUserID is the user ID used for ADK sessions.
	adkUserID = "user"
)

type SessionAgentCall struct {
	SessionID       string
	Prompt          string
	Attachments     []message.Attachment
	MaxOutputTokens int64
	Temperature     *float64
	TopP            *float64
	TopK            *int64
	// PublishedMsgID is set when the user message was already published to the
	// broker at queue time (busy-path). When non-empty, Run() skips re-publishing
	// the user message to avoid duplicates in the chat UI.
	PublishedMsgID string
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) (*AgentResult, error)
	SetModels(large Model, small Model)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	ExecuteUserShell(ctx context.Context, sessionID, command string) error
	Model() Model
	TurnMetrics(sessionID string) *TurnMetrics
	StopProcess(ctx context.Context, sessionID string)
	StopAllProcesses(ctx context.Context)
	SetSessionWorktree(sessionID, cwd, branch string)
	PurgeSession(sessionID string)
}

// Model holds an ADK model and its metadata.
type Model struct {
	LLM      adkmodel.LLM         // ADK model for API calls
	Metadata config.ModelMetadata // model metadata (costs, context window, etc.)
	ModelCfg config.SelectedModel // user's model selection config
	Auth     config.AuthInfo      // resolved auth state (backend, method, user)
}

// TaskHandle pairs a unique identity with a cancel function so that
// concurrent cleanup can verify ownership before deleting map entries.
type TaskHandle struct {
	ID     string
	Cancel context.CancelFunc
}

// NewTaskHandle creates a TaskHandle with a fresh UUID and the given cancel func.
func NewTaskHandle(cancel context.CancelFunc) *TaskHandle {
	return &TaskHandle{ID: uuid.New().String(), Cancel: cancel}
}

type sessionAgent struct {
	agentDef           config.Agent
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]

	isSubAgent        bool
	workingDir        string
	sessions          session.Service
	messageBroker     *pubsub.Broker[message.Message]
	adkSessionService adksession.Service
	artifactService   artifact.Service
	askUserService    askuser.Service
	permissionService permission.Service
	holdFlag          *atomic.Bool
	notifier          *notify.Notifier

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, *TaskHandle]
	turnMetrics    *csync.Map[string, *TurnMetrics]

	// activeADKSessions caches the ADK session pointer by session ID while a
	// run is in progress. The mid-loop plugin reads this to call AppendEvent
	// on the same *localSession object that the runner uses internally.
	activeADKSessions *csync.Map[string, adksession.Session]

	// stations holds one processManager per configured station.
	stations map[string]*processManager
	// steeringConfig maps station name → steering text for ephemeral reminders.
	steeringConfig map[string]string

	// lastTodoUpdate tracks per-session when todos were last updated.
	// Used by staleTodosReminder to inject a steering reminder when stale.
	lastTodoUpdate *csync.Map[string, time.Time]

	// worktreeInfos stores per-session worktree info for CWD injection.
	worktreeInfos *csync.Map[string, *worktreeInfo]
}

// worktreeInfo holds session-specific worktree data for per-turn injection.
type worktreeInfo struct {
	ResolvedCWD string
	Branch      string
}

type SessionAgentOptions struct {
	AgentDef           config.Agent
	Stations           map[string]config.StationConfig
	LargeModel         Model
	SmallModel         Model
	SystemPromptPrefix string
	SystemPrompt       string
	IsSubAgent         bool
	Sessions           session.Service
	MessageBroker      *pubsub.Broker[message.Message]
	ADKSessionService  adksession.Service
	ArtifactService    artifact.Service
	AskUserService     askuser.Service
	PermissionService  permission.Service
	HoldFlag           *atomic.Bool
	WorkingDir         string
	Config             *config.Config // for model catalog lookups (context window)
}

func NewSessionAgent(opts SessionAgentOptions) SessionAgent {
	// Build one processManager per enabled station and collect steering config.
	stations := make(map[string]*processManager, len(opts.Stations))
	steeringCfg := make(map[string]string, len(opts.Stations))
	for name, cfg := range opts.Stations {
		if cfg.Disabled {
			continue
		}
		ctxWindow := contextWindowForStation(opts.Config, cfg)
		stations[name] = newStationProcessManager(name, opts.WorkingDir, cfg, ctxWindow)
		if cfg.Steering != "" {
			steeringCfg[name] = cfg.Steering
		}
	}

	return &sessionAgent{
		agentDef:           opts.AgentDef,
		largeModel:         csync.NewValue(opts.LargeModel),
		smallModel:         csync.NewValue(opts.SmallModel),
		systemPromptPrefix: csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:       csync.NewValue(opts.SystemPrompt),
		isSubAgent:         opts.IsSubAgent,
		workingDir:         opts.WorkingDir,
		sessions:           opts.Sessions,
		messageBroker:      opts.MessageBroker,
		adkSessionService:  opts.ADKSessionService,
		artifactService:    opts.ArtifactService,
		askUserService:     opts.AskUserService,
		permissionService:  opts.PermissionService,
		holdFlag:           opts.HoldFlag,
		notifier: notify.New(
			notify.WithMaxBatch(10),
			notify.WithInstruction("[system] messages are runtime notifications from the Crucible control system."),
		),
		messageQueue:      csync.NewMap[string, []SessionAgentCall](),
		activeRequests:    csync.NewMap[string, *TaskHandle](),
		turnMetrics:       csync.NewMap[string, *TurnMetrics](),
		activeADKSessions: csync.NewMap[string, adksession.Session](),
		stations:          stations,
		steeringConfig:    steeringCfg,
		lastTodoUpdate:    csync.NewMap[string, time.Time](),
		worktreeInfos:     csync.NewMap[string, *worktreeInfo](),
	}
}

// SetSessionWorktree configures worktree isolation for a session.
// Sets per-session CWD on all station processManagers and stores worktree info
// for per-turn system prompt injection.
func (a *sessionAgent) SetSessionWorktree(sessionID, cwd, branch string) {
	for _, pm := range a.stations {
		pm.SetSessionCWD(sessionID, cwd)
	}
	a.worktreeInfos.Set(sessionID, &worktreeInfo{
		ResolvedCWD: cwd,
		Branch:      branch,
	})
}

// PurgeSession removes all per-session worktree state (CWD overrides, worktree info).
func (a *sessionAgent) PurgeSession(sessionID string) {
	for _, pm := range a.stations {
		pm.PurgeSessionCWD(sessionID)
	}
	a.worktreeInfos.Del(sessionID)
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (*AgentResult, error) {
	genCtx, handle, err := a.claimSession(ctx, call)
	if err != nil {
		return nil, err
	}
	if genCtx == nil {
		return nil, nil // queued
	}
	// Safety-net cleanup (idempotent — explicit release in finishRun takes precedence).
	defer handle.Cancel()
	defer a.activeRequests.DeleteFunc(call.SessionID, func(h *TaskHandle) bool {
		return h != nil && h.ID == handle.ID
	})
	var metrics *TurnMetrics
	defer a.turnMetrics.DeleteFunc(call.SessionID, func(tm *TurnMetrics) bool {
		return tm == metrics
	})

	largeModel := a.largeModel.Get()
	systemPrompt := a.systemPrompt.Get()
	if prefix := a.systemPromptPrefix.Get(); prefix != "" {
		systemPrompt = prefix + "\n\n" + systemPrompt
	}

	currentSession, err := a.sessions.Get(ctx, call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	defer a.maybeGenerateTitle(ctx, currentSession.Title, call.SessionID, call.Prompt)()

	// Staleness reminder: append directly to this run's system prompt.
	if reminder := a.staleTodosReminder(currentSession); reminder != "" {
		systemPrompt += reminder
	}

	adkSess, err := a.ensureADKSession(ctx, call.SessionID)
	if err != nil {
		return nil, err
	}
	a.activeADKSessions.Set(call.SessionID, adkSess)
	defer a.activeADKSessions.Del(call.SessionID)

	if call.PublishedMsgID == "" {
		a.publishUserMessage(call)
	}
	metrics = &TurnMetrics{StartTime: time.Now()}
	a.turnMetrics.Set(call.SessionID, metrics)

	turnAbort := new(atomic.Bool)
	cleanupMCP := a.registerMCPAbort(call.SessionID, turnAbort)
	defer cleanupMCP()

	tools, err := a.buildToolSet(call.SessionID, largeModel, turnAbort)
	if err != nil {
		return nil, err
	}
	r, err := a.buildAgentAndRunner(largeModel, systemPrompt, call, tools)
	if err != nil {
		return nil, err
	}

	result, runErr := a.runEventLoop(genCtx, r, adkSess, a.buildUserContent(call), call.SessionID, largeModel, turnAbort)
	a.saveSessionUsage(ctx, largeModel, &currentSession, result.TotalUsage)

	return a.finishRun(ctx, call.SessionID, handle, metrics, &result, runErr, turnAbort)
}

// finishRun releases the session claim and either drains the queue or stops on abort/error.
func (a *sessionAgent) finishRun(
	ctx context.Context, sessionID string, handle *TaskHandle, metrics *TurnMetrics,
	result *AgentResult, runErr error, turnAbort *atomic.Bool,
) (*AgentResult, error) {
	// Explicit release BEFORE drain — enables recursive Run() to re-claim.
	a.activeRequests.DeleteFunc(sessionID, func(h *TaskHandle) bool {
		return h != nil && h.ID == handle.ID
	})
	handle.Cancel()
	a.turnMetrics.DeleteFunc(sessionID, func(tm *TurnMetrics) bool {
		return tm == metrics
	})

	if runErr != nil {
		return nil, runErr
	}

	// Dialog cancellation: clear queue and stop — don't auto-drain.
	// Publish canceled assistant messages for any already-published queued user
	// messages so they don't sit orphaned in the chat UI.
	if turnAbort.Load() {
		a.cancelQueuedMessages(sessionID)
		return result, nil
	}

	return a.drainQueue(ctx, sessionID, result)
}

// cancelQueuedMessages takes all queued calls for a session and publishes a
// canceled assistant message for each one whose user message was already
// published to the broker. Without this, orphaned user messages would sit
// in the chat UI with no response after a dialog-abort clears the queue.
func (a *sessionAgent) cancelQueuedMessages(sessionID string) {
	queued, ok := a.messageQueue.Take(sessionID)
	if !ok {
		return
	}
	for _, call := range queued {
		if call.PublishedMsgID == "" {
			continue // user message was never published — nothing to clean up
		}
		cancelMsg := newInMemoryMessage(sessionID, message.Assistant, nil, "", "")
		cancelMsg.AddFinish(message.FinishReasonCanceled, "Dialog canceled by user", "")
		a.messageBroker.Publish(pubsub.CreatedEvent, cancelMsg.Clone())
	}
}

// registerMCPAbort registers a per-session abort flag with the MCP registry
// and returns a cleanup function. Safe to call when Registry is nil.
func (a *sessionAgent) registerMCPAbort(sessionID string, flag *atomic.Bool) func() {
	if mcp.Registry != nil {
		mcp.Registry.SetTurnAbort(sessionID, flag)
		return func() { mcp.Registry.ClearTurnAbort(sessionID) }
	}
	return func() {}
}

// claimSession validates the call and atomically claims ownership of the session.
// Returns (context, *TaskHandle, nil) on success.
// Returns (nil, nil, nil) if the message was queued (session busy).
// Returns (nil, nil, err) on validation error.
func (a *sessionAgent) claimSession(
	ctx context.Context, call SessionAgentCall,
) (context.Context, *TaskHandle, error) {
	if call.Prompt == "" && len(call.Attachments) == 0 {
		return nil, nil, ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return nil, nil, ErrSessionMissing
	}

	genCtx, cancel := context.WithCancel(ctx)
	handle := NewTaskHandle(cancel)
	if !a.activeRequests.SetNX(call.SessionID, handle) {
		cancel() // we didn't claim — release the cancel func

		userParts := []message.ContentPart{message.TextContent{Text: call.Prompt}}
		for _, att := range call.Attachments {
			userParts = append(userParts, message.BinaryContent{
				Path:     att.FilePath,
				MIMEType: att.MimeType,
				Data:     att.Content,
			})
		}
		userMsg := newInMemoryMessage(call.SessionID, message.User, userParts, "", "")
		userMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		a.messageBroker.Publish(pubsub.CreatedEvent, userMsg.Clone())

		call.PublishedMsgID = userMsg.ID
		csync.AppendSlice(a.messageQueue, call.SessionID, call)
		return nil, nil, nil
	}

	return genCtx, handle, nil
}

// publishUserMessage creates a user message in-memory and publishes to broker for UI.
func (a *sessionAgent) publishUserMessage(call SessionAgentCall) {
	userParts := make([]message.ContentPart, 1, 1+len(call.Attachments))
	userParts[0] = message.TextContent{Text: call.Prompt}
	for _, att := range call.Attachments {
		userParts = append(userParts, message.BinaryContent{
			Path:     att.FilePath,
			MIMEType: att.MimeType,
			Data:     att.Content,
		})
	}
	userMsg := newInMemoryMessage(call.SessionID, message.User, userParts, "", "")
	userMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	a.messageBroker.Publish(pubsub.CreatedEvent, userMsg.Clone())
}

// buildToolSet constructs the supervisor's tool set for this turn.
func (a *sessionAgent) buildToolSet(sessionID string, largeModel Model, turnAbort *atomic.Bool) ([]tool.Tool, error) {
	var tools []tool.Tool
	// Google Search (built-in tool) cannot be combined with function calling
	// on the Gemini Developer API (Gemini 3+). Only enable on Vertex AI.
	if !largeModel.Metadata.DisableSearch && largeModel.Auth.Backend == config.GeminiBackendVertex {
		tools = append(tools, geminitool.GoogleSearch{})
	}
	if a.artifactService != nil {
		tools = append(tools, loadartifactstool.New())
	}
	for name, pm := range a.stations {
		stationTool, err := newStationTool(pm, sessionID, pm.description, a.permissionService, a.holdFlag, a.notifier, turnAbort)
		if err != nil {
			return nil, fmt.Errorf("failed to create station tool %q: %w", name, err)
		}
		tools = append(tools, stationTool)
	}
	if a.askUserService != nil && !a.askUserService.NonInteractive() {
		askTool, err := newAskUserTool(a.askUserService, sessionID, turnAbort)
		if err != nil {
			return nil, fmt.Errorf("failed to create ask_user tool: %w", err)
		}
		tools = append(tools, askTool)
	}
	thoughtTool, err := newThoughtTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create thought tool: %w", err)
	}
	tools = append(tools, thoughtTool)
	todosTool, err := newTodosTool(a.sessions, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create todos tool: %w", err)
	}
	tools = append(tools, todosTool)
	return tools, nil
}

// buildAgentAndRunner constructs the ADK LLM agent and runner for a turn.
func (a *sessionAgent) buildAgentAndRunner(
	largeModel Model,
	systemPrompt string,
	call SessionAgentCall,
	tools []tool.Tool,
) (*runner.Runner, error) {
	var mcpToolsets []tool.Toolset
	if mcp.Registry != nil {
		mcpToolsets = mcp.Registry.Toolsets()
	}
	// Use InstructionProvider instead of Instruction to bypass ADK's
	// {placeholder} substitution — context files embedded in the prompt may
	// contain literal {word} patterns that ADK would try to resolve as
	// session state keys, causing "state key does not exist" errors.
	llmAgent, err := llmagent.New(llmagent.Config{
		Name:        a.agentDef.Name,
		Description: a.agentDef.Description,
		Model:       largeModel.LLM,
		InstructionProvider: func(adkagent.ReadonlyContext) (string, error) {
			return systemPrompt, nil
		},
		GenerateContentConfig: a.buildGenConfig(largeModel, call),
		Tools:                 tools,
		Toolsets:              mcpToolsets,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// Plugin order: midloop → notify → steering → stall_recovery → retry → logging.
	midloop := newMidLoopPlugin(a.messageQueue, a.activeADKSessions, a.adkSessionService)
	stallRecovery := newStallRecoveryPlugin()
	retryPlug := newRetryPlugin(largeModel.LLM, DefaultRetryTransportConfig())
	plugins := []*plugin.Plugin{midloop, a.notifier.Plugin(), stallRecovery, retryPlug, newLoggingPlugin()}
	if len(a.steeringConfig) > 0 {
		plugins = []*plugin.Plugin{
			midloop,
			a.notifier.Plugin(),
			newSteeringPlugin(a.steeringConfig),
			stallRecovery,
			retryPlug,
			newLoggingPlugin(),
		}
	}

	r, err := runner.New(runner.Config{
		AppName:         adkAppName,
		Agent:           llmAgent,
		SessionService:  a.adkSessionService,
		ArtifactService: a.artifactService,
		PluginConfig: runner.PluginConfig{
			Plugins: plugins,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}
	return r, nil
}

// runEventLoop runs the ADK runner, processes events, and handles errors.
// Publishes assistant messages via the broker as they stream. On tool-cycle
// completion (FunctionResponse), creates a fresh assistant message so
// post-tool text renders separately.
func (a *sessionAgent) runEventLoop(
	ctx context.Context,
	r *runner.Runner,
	adkSess adksession.Session,
	userContent *genai.Content,
	sessionID string,
	largeModel Model,
	turnAbort *atomic.Bool,
) (AgentResult, error) {
	assistantMsg := publishAssistantPlaceholder(a.messageBroker, sessionID, largeModel.ModelCfg.Model, largeModel.ModelCfg.Provider)

	startTime := time.Now()
	a.eventPromptSent(sessionID)

	var result AgentResult
	var lastErr error
	ep := newEventProcessor(a.messageBroker, a.turnMetrics, &assistantMsg, &result, a.sessions, a.lastTodoUpdate, turnAbort)
	for event, eventErr := range r.Run(ctx, adkUserID, adkSess.ID(), userContent, adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeSSE,
	}) {
		if eventErr != nil {
			lastErr = eventErr
			break
		}
		if event == nil {
			continue
		}
		if ep.process(event) {
			break
		}
		// When a tool cycle completes (FunctionResponse received), start a new
		// assistant message so post-tool text renders after the tool in the chat.
		if eventHasFunctionResponse(event) {
			assistantMsg = publishAssistantPlaceholder(a.messageBroker, sessionID, largeModel.ModelCfg.Model, largeModel.ModelCfg.Provider)
			ep.msg = &assistantMsg
		}
	}

	a.eventPromptResponded(sessionID, time.Since(startTime).Truncate(time.Second))

	// Handle dialog cancellation — tool signaled abort after its result was processed.
	if turnAbort != nil && turnAbort.Load() {
		assistantMsg.FinishThinking()
		assistantMsg.CancelPendingToolCalls()
		assistantMsg.AddFinish(message.FinishReasonCanceled, "Dialog canceled by user", "")
		ep.throttle.flush(&assistantMsg)
		return result, nil
	}

	if lastErr != nil {
		assistantMsg.FinishThinking()
		if errors.Is(lastErr, context.Canceled) {
			assistantMsg.CancelPendingToolCalls()
			assistantMsg.AddFinish(message.FinishReasonCanceled, "User canceled request", "")
		} else {
			assistantMsg.AddFinish(message.FinishReasonError, "Provider Error", lastErr.Error())
		}
		ep.throttle.flush(&assistantMsg)
		return result, lastErr
	}

	finalizeAssistantMessage(ep.throttle, &assistantMsg, result.TotalUsage)
	return result, nil
}

// drainQueue processes queued messages after the current turn completes.
// PRECONDITION: the caller has already released the session claim via
// activeRequests.Del(). Without this, the recursive Run() would re-queue.
func (a *sessionAgent) drainQueue(
	ctx context.Context, sessionID string, result *AgentResult,
) (*AgentResult, error) {
	queued, ok := a.messageQueue.Take(sessionID)
	if !ok || len(queued) == 0 {
		return result, nil
	}
	first := queued[0]
	if len(queued) > 1 {
		a.messageQueue.Set(sessionID, queued[1:])
	}
	return a.Run(ctx, first)
}

// ensureADKSession gets or creates an ADK session for the given Crucible session ID.
func (a *sessionAgent) ensureADKSession(ctx context.Context, sessionID string) (adksession.Session, error) {
	resp, err := a.adkSessionService.Get(ctx, &adksession.GetRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err == nil {
		return resp.Session, nil
	}
	// Only fall through to Create if the error indicates the session doesn't exist.
	// Propagate real errors (DB corruption, timeouts, etc.) to avoid silently
	// creating a new empty session and losing conversation history.
	if !isSessionNotFoundError(err) {
		return nil, fmt.Errorf("get ADK session: %w", err)
	}
	createResp, err := a.adkSessionService.Create(ctx, &adksession.CreateRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("create ADK session: %w", err)
	}
	return createResp.Session, nil
}

// isSessionNotFoundError checks whether an error from ADK's session.Service.Get
// indicates the session simply doesn't exist (vs a real infrastructure error).
// ADK's InMemoryService returns "session ... not found" and the database service
// wraps gorm.ErrRecordNotFound with "database error while fetching session".
func isSessionNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "record not found")
}

// Summarize is a no-op. Summarization is deferred to ADK's own compaction.
func (a *sessionAgent) Summarize(_ context.Context, _ string) error {
	return nil
}

func (a *sessionAgent) Cancel(sessionID string) {
	if h, ok := a.activeRequests.Get(sessionID); ok && h != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		h.Cancel()
	}

	if h, ok := a.activeRequests.Get(sessionID + "-summarize"); ok && h != nil {
		slog.Debug("Summarize cancellation initiated", "session_id", sessionID)
		h.Cancel()
	}

	if h, ok := a.activeRequests.Get(sessionID + "-shell"); ok && h != nil {
		slog.Debug("Shell cancellation initiated", "session_id", sessionID)
		h.Cancel()
	}

	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}

	// Stop any running station processes for this session.
	a.StopProcess(context.Background(), sessionID)
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}
}

func (a *sessionAgent) CancelAll() {
	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key)
	}

	timeout := time.After(5 * time.Second)
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *sessionAgent) IsBusy() bool {
	var busy bool
	for h := range a.activeRequests.Seq() {
		if h != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Get(sessionID)
	return busy
}

func (a *sessionAgent) TurnMetrics(sessionID string) *TurnMetrics {
	tm, _ := a.turnMetrics.Get(sessionID)
	return tm
}

func (a *sessionAgent) QueuedPrompts(sessionID string) int {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return 0
	}
	return len(l)
}

func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return nil
	}
	prompts := make([]string, len(l))
	for i, call := range l {
		prompts[i] = call.Prompt
	}
	return prompts
}

func (a *sessionAgent) SetModels(large Model, small Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
}

const todoStaleThreshold = 5 * time.Minute

// staleTodosReminder returns a steering XML block if the session's todos
// are stale, or "" if no reminder is needed. The caller appends the result
// to the per-run systemPrompt local variable — no shared state touched.
func (a *sessionAgent) staleTodosReminder(sess session.Session) string {
	if !session.HasIncompleteTodos(sess.Todos) {
		return ""
	}
	lastUpdate, ok := a.lastTodoUpdate.Get(sess.ID)
	if !ok {
		// First check for this session (or after restart).
		// Seed the timer — don't remind immediately.
		a.lastTodoUpdate.Set(sess.ID, time.Now())
		return ""
	}
	if time.Since(lastUpdate) < todoStaleThreshold {
		return ""
	}
	return "\n<steering_reminder>\nYour todo list may be stale — review and update it to reflect current progress.\n</steering_reminder>"
}

func (a *sessionAgent) StopProcess(ctx context.Context, sessionID string) {
	for _, pm := range a.stations {
		pm.stop(ctx, sessionID)
	}
}

func (a *sessionAgent) StopAllProcesses(ctx context.Context) {
	for _, pm := range a.stations {
		pm.stopAll(ctx)
	}
}
