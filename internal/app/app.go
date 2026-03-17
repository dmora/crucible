// Package app wires together services, coordinates agents, and manages
// application lifecycle.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/charmbracelet/x/term"
	artifactsqlite "github.com/dmora/adk-go-extras/artifact/sqlite"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/agent/tools/mcp"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/event"
	"github.com/dmora/crucible/internal/filetracker"
	"github.com/dmora/crucible/internal/format"
	"github.com/dmora/crucible/internal/history"
	"github.com/dmora/crucible/internal/log"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/permission"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
	"github.com/dmora/crucible/internal/shell"
	"github.com/dmora/crucible/internal/ui/anim"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/dmora/crucible/internal/update"
	"github.com/dmora/crucible/internal/version"
	"github.com/dmora/crucible/internal/worktree"
	glebarezsqlite "github.com/glebarez/sqlite"
	"google.golang.org/adk/artifact"
	adksession "google.golang.org/adk/session"
	adkdatabase "google.golang.org/adk/session/database"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// UpdateAvailableMsg is sent when a new version is available.
type UpdateAvailableMsg struct {
	CurrentVersion string
	LatestVersion  string
	IsDevelopment  bool
}

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service
	AskUser     askuser.Service
	FileTracker filetracker.Service

	AgentCoordinator agent.Coordinator

	config *config.Config

	// ADK session infrastructure (passed to agent layer).
	messageBroker     *pubsub.Broker[message.Message]
	adkSessionService adksession.Service
	artifactService   artifact.Service

	serviceEventsWG *sync.WaitGroup
	eventsCtx       context.Context
	events          chan tea.Msg
	tuiWG           *sync.WaitGroup

	// worktreeManager is non-nil when worktree isolation is enabled.
	worktreeManager *worktree.Manager

	// global context and cleanup functions
	globalCtx    context.Context
	cleanupFuncs []func(context.Context) error
}

// New initializes a new application instance.
func New(ctx context.Context, conn *sql.DB, cfg *config.Config) (*App, error) {
	q := db.New(conn)

	// Create GORM-backed ADK session service in a separate DB file.
	// ADK's GORM models use table name "sessions" which collides with Crucible's
	// goose-managed "sessions" table, so they must live in separate databases.
	adkDBPath := filepath.Join(cfg.Options.DataDirectory, "crucible-adk.db")
	rawAdkSessionSvc, err := adkdatabase.NewSessionService(glebarezsqlite.Open(adkDBPath), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ADK session service: %w", err)
	}
	if err := adkdatabase.AutoMigrate(rawAdkSessionSvc); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate ADK tables: %w", err)
	}

	// Second GORM connection to same file for artifact service.
	// ADK encapsulates its *gorm.DB; we need our own handle for the
	// adk-go-extras artifact table. SQLite WAL mode supports concurrent access.
	artifactDB, err := gorm.Open(glebarezsqlite.Open(adkDBPath), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open artifact DB: %w", err)
	}
	artifactSvc, err := artifactsqlite.NewService(artifactDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create artifact service: %w", err)
	}

	// Hoist app pointer so the delete callback can capture it. The pointer
	// is nil at definition time but populated before any delete can fire.
	var app *App

	// Crucible session service with full teardown on delete.
	// PreDelete runs BEFORE the DB transaction — cancels agents and purges state.
	// OnDelete runs AFTER DB commit — cleans up external ADK session data.
	sessions := session.NewService(q, conn,
		session.WithPreDelete(func(ctx context.Context, id string) {
			// 1. Cancel active request + summarize + clear queued prompts.
			if app != nil && app.AgentCoordinator != nil {
				app.AgentCoordinator.Cancel(id)
			}
			// 2. Kill station subprocesses via agentrun.
			if app != nil && app.AgentCoordinator != nil {
				app.AgentCoordinator.StopProcess(ctx, id)
			}
			// 3. Purge in-memory process state entries (publishes UI events).
			agent.PurgeSessionProcessStates(id)
		}),
		session.WithOnDelete(func(ctx context.Context, id string) {
			// 4. Delete ADK session (conversation data) — after DB commit.
			if deleteErr := rawAdkSessionSvc.Delete(ctx, &adksession.DeleteRequest{
				AppName:   "crucible",
				UserID:    "user",
				SessionID: id,
			}); deleteErr != nil {
				slog.Warn("Failed to delete ADK session", "session_id", id, "error", deleteErr)
			}
			// 5. Clean up worktree (after DB commit — safe from rollback).
			if app != nil && app.worktreeManager != nil {
				if wtErr := app.worktreeManager.Cleanup(id); wtErr != nil {
					slog.Warn("worktree cleanup failed", "session_id", id, "error", wtErr)
				}
			}
			// 6. Purge per-session worktree caches (CWD overrides, worktreeInfos).
			if app != nil && app.AgentCoordinator != nil {
				app.AgentCoordinator.PurgeSession(id)
			}
		}),
	)

	files := history.NewService(q, conn)
	skipPermissionsRequests := cfg.Permissions != nil && cfg.Permissions.SkipRequests
	var allowedTools []string
	if cfg.Permissions != nil && cfg.Permissions.AllowedTools != nil {
		allowedTools = cfg.Permissions.AllowedTools
	}

	// Create standalone message broker — shared between agent (publisher) and UI (subscriber).
	messageBroker := pubsub.NewBroker[message.Message]()

	// Create ADK-backed message service: reads from ADK events, publishes to broker.
	messages := agent.NewADKMessageService(rawAdkSessionSvc, messageBroker)

	app = &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permission.NewPermissionService(cfg.WorkingDir(), skipPermissionsRequests, allowedTools),
		AskUser:     askuser.NewService(false),
		FileTracker: filetracker.NewService(q),

		globalCtx: ctx,

		config: cfg,

		messageBroker:     messageBroker,
		adkSessionService: agent.NewRetrySessionService(rawAdkSessionSvc),
		artifactService:   artifactSvc,

		events:          make(chan tea.Msg, 100),
		serviceEventsWG: &sync.WaitGroup{},
		tuiWG:           &sync.WaitGroup{},
	}

	app.setupEvents()

	// Check for updates in the background.
	go app.checkForUpdates(ctx)

	go mcp.Initialize(ctx, app.Permissions, cfg)

	// cleanup database upon app shutdown
	app.cleanupFuncs = append(
		app.cleanupFuncs,
		func(context.Context) error { return conn.Close() },
		mcp.Close,
	)

	if err := app.InitCoderAgent(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize coder agent: %w", err)
	}

	// Worktree isolation: create manager, prune orphans, hydrate cache.
	if cfg.Options.Worktree {
		app.initWorktrees(ctx)
	}

	return app, nil
}

// Config returns the application configuration.
func (app *App) Config() *config.Config {
	return app.config
}

// ArtifactService returns the artifact persistence service.
func (app *App) ArtifactService() artifact.Service {
	return app.artifactService
}

// RunNonInteractive runs the application in non-interactive mode with the
// given prompt, printing to stdout.
func (app *App) RunNonInteractive(ctx context.Context, output io.Writer, prompt, largeModel, smallModel string, hideSpinner bool) error {
	slog.Info("Running in non-interactive mode")

	app.AskUser.SetNonInteractive(true)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if largeModel != "" || smallModel != "" {
		if err := app.overrideModelsForNonInteractive(ctx, largeModel, smallModel); err != nil {
			return fmt.Errorf("failed to override models: %w", err)
		}
	}

	var (
		spinner   *format.Spinner
		stdoutTTY bool
		stderrTTY bool
		stdinTTY  bool
		progress  bool
	)

	if f, ok := output.(*os.File); ok {
		stdoutTTY = term.IsTerminal(f.Fd())
	}
	stderrTTY = term.IsTerminal(os.Stderr.Fd())
	stdinTTY = term.IsTerminal(os.Stdin.Fd())
	progress = app.config.Options.Progress == nil || *app.config.Options.Progress

	if !hideSpinner && stderrTTY {
		t := styles.NewStyles(styles.ThemeID(app.config.ThemeID()), app.config.IsTransparent())

		// Detect background color to set the appropriate color for the
		// spinner's 'Generating...' text. Without this, that text would be
		// unreadable in light terminals.
		hasDarkBG := true
		if f, ok := output.(*os.File); ok && stdinTTY && stdoutTTY {
			hasDarkBG = lipgloss.HasDarkBackground(os.Stdin, f)
		}
		defaultFG := lipgloss.LightDark(hasDarkBG)(charmtone.Pepper, t.FgBase)

		preset := app.config.SpinnerPreset()
		spinner = format.NewSpinner(ctx, cancel, preset, anim.Settings{
			Size:        10,
			Label:       "Generating",
			LabelColor:  defaultFG,
			GradColorA:  t.Primary,
			GradColorB:  t.Secondary,
			GradColorC:  t.Tertiary,
			CycleColors: true,
		})
		spinner.Start()
	}

	// Helper function to stop spinner once.
	stopSpinner := func() {
		if !hideSpinner && spinner != nil {
			spinner.Stop()
			spinner = nil
		}
	}

	// Wait for MCP initialization to complete before reading MCP tools.
	if err := mcp.WaitForInit(ctx); err != nil {
		return fmt.Errorf("failed to wait for MCP initialization: %w", err)
	}

	// force update of agent models before running so mcp tools are loaded
	if err := app.AgentCoordinator.UpdateModels(ctx); err != nil {
		return fmt.Errorf("failed to update agent models: %w", err)
	}

	defer stopSpinner()

	const maxPromptLengthForTitle = 100
	const titlePrefix = "Non-interactive: "
	var titleSuffix string

	if len(prompt) > maxPromptLengthForTitle {
		titleSuffix = prompt[:maxPromptLengthForTitle] + "..."
	} else {
		titleSuffix = prompt
	}
	title := titlePrefix + titleSuffix

	sess, err := app.Sessions.Create(ctx, title)
	if err != nil {
		return fmt.Errorf("failed to create session for non-interactive mode: %w", err)
	}
	slog.Info("Created session for non-interactive run", "session_id", sess.ID)

	// Automatically approve all permission requests for this non-interactive
	// session.
	app.Permissions.AutoApproveSession(sess.ID)

	type response struct {
		result *agent.AgentResult
		err    error
	}
	done := make(chan response, 1)

	go func(ctx context.Context, sessionID, prompt string) {
		result, err := app.AgentCoordinator.Run(ctx, sess.ID, prompt)
		if err != nil {
			done <- response{
				err: fmt.Errorf("failed to start agent processing stream: %w", err),
			}
			return
		}
		done <- response{
			result: result,
		}
	}(ctx, sess.ID, prompt)

	messageEvents := app.Messages.Subscribe(ctx)
	messageReadBytes := make(map[string]int)
	var printed bool

	defer func() {
		if progress && stderrTTY {
			_, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar)
		}

		// Always print a newline at the end. If output is a TTY this will
		// prevent the prompt from overwriting the last line of output.
		_, _ = fmt.Fprintln(output)
	}()

	for {
		if progress && stderrTTY {
			// HACK: Reinitialize the terminal progress bar on every iteration
			// so it doesn't get hidden by the terminal due to inactivity.
			_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
		}

		select {
		case result := <-done:
			stopSpinner()
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, agent.ErrRequestCancelled) {
					slog.Debug("Non-interactive: agent processing cancelled", "session_id", sess.ID)
					return nil
				}
				return fmt.Errorf("agent processing failed: %w", result.err)
			}
			return nil

		case event := <-messageEvents:
			msg := event.Payload
			if msg.SessionID == sess.ID && msg.Role == message.Assistant && len(msg.Parts) > 0 {
				stopSpinner()

				content := msg.Content().String()
				readBytes := messageReadBytes[msg.ID]

				if len(content) < readBytes {
					slog.Error("Non-interactive: message content is shorter than read bytes", "message_length", len(content), "read_bytes", readBytes)
					return fmt.Errorf("message content is shorter than read bytes: %d < %d", len(content), readBytes)
				}

				part := content[readBytes:]
				// Trim leading whitespace. Sometimes the LLM includes leading
				// formatting and intentation, which we don't want here.
				if readBytes == 0 {
					part = strings.TrimLeft(part, " \t")
				}
				// Ignore initial whitespace-only messages.
				if printed || strings.TrimSpace(part) != "" {
					printed = true
					fmt.Fprint(output, part)
				}
				messageReadBytes[msg.ID] = len(content)
			}

		case <-ctx.Done():
			stopSpinner()
			return ctx.Err()
		}
	}
}

func (app *App) UpdateAgentModel(ctx context.Context) error {
	if app.AgentCoordinator == nil {
		return fmt.Errorf("agent configuration is missing")
	}
	return app.AgentCoordinator.UpdateModels(ctx)
}

// overrideModelsForNonInteractive parses the model strings and temporarily
// overrides the model configurations, then rebuilds the agent.
// Format: "model-name" (searches all providers) or "provider/model-name".
// Model matching is case-insensitive.
// If largeModel is provided but smallModel is not, the small model defaults to
// the provider's default small model.
func (app *App) overrideModelsForNonInteractive(ctx context.Context, largeModel, smallModel string) error {
	providers := app.config.Providers.Copy()

	largeMatches, smallMatches, err := findModels(providers, largeModel, smallModel)
	if err != nil {
		return err
	}

	var largeProviderID string

	// Override large model.
	if largeModel != "" {
		found, err := validateMatches(largeMatches, largeModel, "large")
		if err != nil {
			return err
		}
		largeProviderID = found.provider
		slog.Info("Overriding large model for non-interactive run", "provider", found.provider, "model", found.modelID)
		app.config.Models[config.SelectedModelTypeLarge] = config.SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}
	}

	// Override small model.
	switch {
	case smallModel != "":
		found, err := validateMatches(smallMatches, smallModel, "small")
		if err != nil {
			return err
		}
		slog.Info("Overriding small model for non-interactive run", "provider", found.provider, "model", found.modelID)
		app.config.Models[config.SelectedModelTypeSmall] = config.SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}

	case largeModel != "":
		// No small model specified, but large model was - use provider's default.
		smallCfg := app.GetDefaultSmallModel(largeProviderID)
		app.config.Models[config.SelectedModelTypeSmall] = smallCfg
	}

	return app.AgentCoordinator.UpdateModels(ctx)
}

// GetDefaultSmallModel returns the default small model for the given
// provider. Falls back to the large model if no default is found.
func (app *App) GetDefaultSmallModel(providerID string) config.SelectedModel {
	cfg := app.config
	largeModelCfg := cfg.Models[config.SelectedModelTypeLarge]

	// Find the provider in the known providers list to get its default small model.
	knownProviders, _ := config.Providers(cfg)
	var knownProvider *config.ProviderMetadata
	for _, p := range knownProviders {
		if p.ID == providerID {
			knownProvider = &p
			break
		}
	}

	// For unknown/local providers, use the large model as small.
	if knownProvider == nil {
		slog.Warn("Using large model as small model for unknown provider", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	defaultSmallModelID := knownProvider.DefaultSmallModelID
	model := cfg.GetModel(providerID, defaultSmallModelID)
	if model == nil {
		slog.Warn("Default small model not found, using large model", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	slog.Info("Using provider default small model", "provider", providerID, "model", defaultSmallModelID)
	return config.SelectedModel{
		Provider:        providerID,
		Model:           defaultSmallModelID,
		MaxTokens:       model.DefaultMaxTokens,
		ReasoningEffort: model.DefaultReasoningEffort,
	}
}

func (app *App) setupEvents() {
	ctx, cancel := context.WithCancel(app.globalCtx)
	app.eventsCtx = ctx
	setupSubscriber(ctx, app.serviceEventsWG, "sessions", app.Sessions.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "messages", app.Messages.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "permissions", app.Permissions.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "permissions-notifications", app.Permissions.SubscribeNotifications, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "history", app.History.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "mcp", mcp.SubscribeEvents, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "processes", agent.SubscribeProcessEvents, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "askuser", app.AskUser.Subscribe, app.events)
	cleanupFunc := func(context.Context) error {
		cancel()
		app.serviceEventsWG.Wait()
		agent.ShutdownProcessBroker()
		return nil
	}
	app.cleanupFuncs = append(app.cleanupFuncs, cleanupFunc)
}

const subscriberSendTimeout = 2 * time.Second

func setupSubscriber[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
	outputCh chan<- tea.Msg,
) {
	wg.Go(func() {
		subCh := subscriber(ctx)
		sendTimer := time.NewTimer(0)
		<-sendTimer.C
		defer sendTimer.Stop()

		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					slog.Debug("Subscription channel closed", "name", name)
					return
				}
				var msg tea.Msg = event
				if !sendTimer.Stop() {
					select {
					case <-sendTimer.C:
					default:
					}
				}
				sendTimer.Reset(subscriberSendTimeout)

				select {
				case outputCh <- msg:
				case <-sendTimer.C:
					slog.Debug("Message dropped due to slow consumer", "name", name)
				case <-ctx.Done():
					slog.Debug("Subscription cancelled", "name", name)
					return
				}
			case <-ctx.Done():
				slog.Debug("Subscription cancelled", "name", name)
				return
			}
		}
	})
}

func (app *App) InitCoderAgent(ctx context.Context) error {
	if _, ok := app.config.Agents[config.AgentCrucible]; !ok {
		return fmt.Errorf("crucible agent configuration is missing")
	}
	var err error
	app.AgentCoordinator, err = agent.NewCoordinator(
		ctx,
		app.config,
		app.Sessions,
		app.messageBroker,
		app.adkSessionService,
		app.artifactService,
		app.AskUser,
		app.Permissions,
	)
	if err != nil {
		slog.Error("Failed to create coder agent", "err", err)
		return err
	}
	return nil
}

// initWorktrees creates the worktree manager, prunes orphans, and hydrates the
// coordinator cache so the UI can display worktree indicators for existing sessions.
func (app *App) initWorktrees(ctx context.Context) {
	mgr, err := worktree.NewManager(app.config.WorkingDir())
	if err != nil {
		slog.Warn("Worktree isolation disabled", "error", err)
		return
	}
	app.worktreeManager = mgr

	// Prune orphaned worktrees from previous runs.
	activeIDs, err := app.Sessions.ListIDs(ctx)
	if err != nil {
		slog.Warn("Failed to list session IDs for worktree prune", "error", err)
	} else if pruneErr := mgr.Prune(activeIDs); pruneErr != nil {
		slog.Warn("Worktree prune failed", "error", pruneErr)
	}

	// Wire manager into coordinator via the callback interface.
	if coord, ok := app.AgentCoordinator.(interface {
		SetWorktreeManager(mgr *agent.WorktreeManagerAdapter)
	}); ok {
		coord.SetWorktreeManager(agent.NewWorktreeManagerAdapter(
			func(sessionID string) (string, string, error) {
				info, err := mgr.Provision(sessionID)
				if err != nil {
					return "", "", err
				}
				return info.ResolvedCWD, info.Branch, nil
			},
			func(sessionID string) (string, string, bool) {
				info, ok := mgr.Status(sessionID)
				if !ok {
					return "", "", false
				}
				return info.ResolvedCWD, info.Branch, true
			},
		))
	}

	// Hydrate coordinator cache for existing sessions.
	for _, id := range activeIDs {
		if info, ok := mgr.Status(id); ok {
			app.AgentCoordinator.SetWorktreeInfo(id, info.ResolvedCWD, info.Branch)
		}
	}
}

// WorktreeManager returns the worktree manager, or nil if disabled.
func (app *App) WorktreeManager() *worktree.Manager {
	return app.worktreeManager
}

// Subscribe sends events to the TUI as tea.Msgs.
func (app *App) Subscribe(program *tea.Program) {
	defer log.RecoverPanic("app.Subscribe", func() {
		slog.Info("TUI subscription panic: attempting graceful shutdown")
		program.Quit()
	})

	app.tuiWG.Add(1)
	tuiCtx, tuiCancel := context.WithCancel(app.globalCtx)
	app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
		slog.Debug("Cancelling TUI message handler")
		tuiCancel()
		app.tuiWG.Wait()
		return nil
	})
	defer app.tuiWG.Done()

	for {
		select {
		case <-tuiCtx.Done():
			slog.Debug("TUI message handler shutting down")
			return
		case msg, ok := <-app.events:
			if !ok {
				slog.Debug("TUI message channel closed")
				return
			}
			program.Send(msg)
		}
	}
}

// Shutdown performs a graceful shutdown of the application.
func (app *App) Shutdown() {
	start := time.Now()
	defer func() { slog.Debug("Shutdown took " + time.Since(start).String()) }()

	// First, cancel all agents and wait for them to finish. This must complete
	// before closing the DB so agents can finish writing their state.
	if app.AgentCoordinator != nil {
		app.AgentCoordinator.CancelAll()
	}

	// Now run remaining cleanup tasks in parallel.
	var wg sync.WaitGroup

	// Shared shutdown context for all timeout-bounded cleanup.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(app.globalCtx), 5*time.Second)
	defer cancel()

	// Send exit event
	wg.Go(func() {
		event.AppExited()
	})

	// Kill all background shells.
	wg.Go(func() {
		shell.GetBackgroundShellManager().KillAll(shutdownCtx)
	})

	// Call all cleanup functions.
	for _, cleanup := range app.cleanupFuncs {
		if cleanup != nil {
			wg.Go(func() {
				if err := cleanup(shutdownCtx); err != nil {
					slog.Error("Failed to cleanup app properly on shutdown", "error", err)
				}
			})
		}
	}
	wg.Wait()
}

// checkForUpdates checks for available updates.
func (app *App) checkForUpdates(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	info, err := update.Check(checkCtx, version.Version, update.Default)
	if err != nil || !info.Available() {
		return
	}
	app.events <- UpdateAvailableMsg{
		CurrentVersion: info.Current,
		LatestVersion:  info.Latest,
		IsDevelopment:  info.IsDevelopment(),
	}
}
