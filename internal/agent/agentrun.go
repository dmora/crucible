package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmora/adk-go-extras/plugin/notify"
	"github.com/dmora/agentrun"
	"github.com/dmora/agentrun/engine/acp"
	"github.com/dmora/agentrun/engine/cli"
	"github.com/dmora/agentrun/engine/cli/claude"
	"github.com/dmora/agentrun/engine/cli/codex"
	"github.com/dmora/agentrun/engine/cli/opencode"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/permission"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// stationInput is the schema for station function tools.
// All fields are required — validation rejects empty values to force the model
// to distribute context across structured fields instead of dumping everything in task.
type stationInput struct {
	Task            string   `json:"task" jsonschema:"required" description:"One-line summary of what to do."`
	TaskDescription string   `json:"task_description" jsonschema:"required" description:"Full context — background, details, specifics, file paths, code snippets. This tool cannot see your conversation — include everything it needs."`
	ContextHints    []string `json:"context_hints" jsonschema:"required" description:"File paths, prior tool results, or decisions to reference."`
	Constraints     []string `json:"constraints" jsonschema:"required" description:"Boundaries and prohibitions."`
	SuccessCriteria []string `json:"success_criteria" jsonschema:"required" description:"Observable outcomes that define done."`
}

// stationIsolationFooter is appended to every station tool description so the
// supervisor sees the constraint at the point of filling tool call parameters.
const stationIsolationFooter = "\n\nIMPORTANT: This tool runs in a separate context and cannot see your conversation. " +
	"Put the one-line goal in task, full details in task_description, " +
	"file paths and prior results in context_hints, boundaries in constraints, " +
	"and done-criteria in success_criteria. All fields are required."

// stationOutput is the return schema for station function tools.
type stationOutput struct {
	Result       string `json:"result,omitempty"        description:"The result"`
	Error        string `json:"error,omitempty"         description:"Error result"`
	ArtifactPath string `json:"artifact_path,omitempty" description:"Path to the primary artifact file produced by this station"`
	Abort        bool   `json:"_abort,omitempty"` // internal marker for reload detection; no description tag
}

// processManager manages persistent agentrun processes for a single station.
// Each session gets at most one agent process per station that persists across LLM turns.
type processManager struct {
	mu          sync.Mutex
	processes   map[string]agentrun.Process // keyed by sessionID
	sessionCWDs map[string]string           // per-session CWD overrides (worktree isolation)
	engine      agentrun.Engine
	station     string // station name (e.g. "plan", "inspect", "build")
	cwd         string
	backend     string
	model       string
	options     map[string]string
	env         map[string]string // extra env vars for the agent process

	description string // tool description for the orchestrator LLM

	// Route enforcement
	requires                   []string // stations that must complete before this one can run
	afterDone                  []string // stations that must run after this one succeeds before it can run again
	requiresArtifact           []string // stations whose active artifact must exist before dispatch
	disableArtifactEnforcement bool     // overrides requiresArtifact when true

	// Harness components
	gate     *GateController
	task     *TaskBuilder
	observer *StationObserver
	recovery *RecoveryController
	persist  *StatePersister
}

// ProcessOps interface implementation — delegates to internal methods + observer.
func (pm *processManager) GetOrStart(ctx context.Context, sessionID, prompt, resumeID string) (agentrun.Process, bool, error) {
	return pm.getOrStart(ctx, sessionID, prompt, resumeID)
}

func (pm *processManager) KillProcess(ctx context.Context, sessionID string) {
	pm.killProcess(ctx, sessionID)
}

func (pm *processManager) DefaultContextWindowSize() int {
	return pm.defaultContextWindowSize()
}

func (pm *processManager) RemoveFromPool(sessionID string) {
	pm.mu.Lock()
	delete(pm.processes, sessionID)
	pm.mu.Unlock()
}

func (pm *processManager) ClearActivity(sessionID string) {
	pm.observer.ClearActivity(sessionID)
}

const (
	defaultBackend     = "claude"
	backendCodex       = "codex"
	backendOpenCode    = "opencode"
	backendOpenCodeACP = "opencode-acp"

	// defaultContextWindow is the safety-net value when the model catalog
	// has no entry for the station's model.
	defaultContextWindow = 200_000
)

// newStationProcessManager creates a processManager for a named station.
// contextWindow is looked up from the model catalog by the caller.
func newStationProcessManager(station, cwd string, cfg config.StationConfig, contextWindow int) *processManager {
	backend := cmp.Or(cfg.Backend, defaultBackend)
	model := cfg.Model // empty = let the CLI pick its default

	// Default: HITL=off → --permission-mode bypassPermissions.
	options := make(map[string]string, len(cfg.Options)+1)
	options[agentrun.OptionHITL] = string(agentrun.HITLOff)
	for k, v := range cfg.Options {
		options[k] = v
	}

	// OpenCode backends need OPENCODE_PERMISSION to allow external directory
	// access when run as a station subprocess.
	var env map[string]string
	if (backend == backendOpenCode || backend == backendOpenCodeACP) && (cfg.Env == nil || cfg.Env["OPENCODE_PERMISSION"] == "") {
		env = make(map[string]string, len(cfg.Env)+1)
		for k, v := range cfg.Env {
			env[k] = v
		}
		env["OPENCODE_PERMISSION"] = `{"external_directory":"allow"}`
	} else {
		env = cfg.Env
	}

	pm := &processManager{
		processes:                  make(map[string]agentrun.Process),
		sessionCWDs:                make(map[string]string),
		engine:                     buildEngine(backend),
		station:                    station,
		cwd:                        cwd,
		backend:                    backend,
		model:                      model,
		options:                    options,
		env:                        env,
		description:                cfg.Description,
		requires:                   cfg.Requires,
		afterDone:                  cfg.AfterDone,
		requiresArtifact:           cfg.RequiresArtifact,
		disableArtifactEnforcement: cfg.DisableArtifactEnforcement,
	}

	pm.gate = &GateController{
		station: station, gated: cfg.Gate, cwd: cwd, cwdResolver: pm.resolvedCWD,
	}
	pm.task = &TaskBuilder{
		station: station, skill: cfg.Skill, backend: backend,
	}
	pm.observer = &StationObserver{
		station: station,
	}
	pm.recovery = &RecoveryController{
		station: station, cwd: cwd, cwdResolver: pm.resolvedCWD,
		model: model, contextWindow: contextWindow,
		captureRepoStateFn: captureRepoState,
	}
	pm.persist = &StatePersister{
		station: station, artifactType: cfg.ArtifactType,
	}

	return pm
}

func buildEngine(backend string) agentrun.Engine {
	switch backend {
	case backendCodex:
		return cli.NewEngine(codex.New())
	case backendOpenCode:
		return cli.NewEngine(opencode.New())
	case backendOpenCodeACP:
		return acp.NewEngine(
			acp.WithBinary("opencode"),
			acp.WithArgs("acp"),
		)
	default:
		return cli.NewEngine(claude.New())
	}
}

// SetSessionCWD sets a per-session CWD override (used for worktree isolation).
func (pm *processManager) SetSessionCWD(sessionID, path string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.sessionCWDs[sessionID] = path
}

// resolvedCWD returns the per-session CWD if set, otherwise the default cwd.
// Acquires pm.mu — do NOT call while holding the lock.
func (pm *processManager) resolvedCWD(sessionID string) string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.resolvedCWDLocked(sessionID)
}

// resolvedCWDLocked is the lock-free version for callers that already hold pm.mu.
func (pm *processManager) resolvedCWDLocked(sessionID string) string {
	if cwd, ok := pm.sessionCWDs[sessionID]; ok {
		return cwd
	}
	return pm.cwd
}

// PurgeSessionCWD removes the per-session CWD override for a deleted session.
func (pm *processManager) PurgeSessionCWD(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.sessionCWDs, sessionID)
}

// stateKey returns the composite key for processStates: "sessionID:station".
func (pm *processManager) stateKey(sessionID string) string {
	return processStateKey(sessionID, pm.station)
}

// stationResumeStateKey returns the ADK session state key for persisting a
// station's CLI resume ID (e.g. "station:plan:resume_id").
func stationResumeStateKey(station string) string {
	return "station:" + station + ":resume_id"
}

// stationStateKey returns the ADK session state key for persisting a
// station's durable state (e.g. "station:plan:state").
func stationStateKey(station string) string {
	return "station:" + station + ":state"
}

// stationDurableState is the JSON-serializable subset of ProcessInfo
// that survives app restarts.
type stationDurableState struct {
	Station     string `json:"station"`
	Backend     string `json:"backend"`
	Model       string `json:"model"`
	ResumeID    string `json:"resume_id,omitempty"`
	ContextUsed int    `json:"context_used,omitempty"`
	ContextSize int    `json:"context_size,omitempty"`
	StartedAt   int64  `json:"started_at,omitempty"` // unix seconds
}

// durableDispatchEntry is the JSON-serializable form of DispatchEntry for
// persistence to ADK session state.
type durableDispatchEntry struct {
	Station      string `json:"station"`
	Verdict      int    `json:"verdict"`
	StartedAt    int64  `json:"started_at"`
	DurationMs   int64  `json:"duration_ms"`
	Seq          int    `json:"seq"`
	ContextUsed  int    `json:"context_used,omitempty"`
	ContextSize  int    `json:"context_size,omitempty"`
	ArtifactPath string `json:"artifact_path,omitempty"`
}

const dispatchLogStateKey = "dispatch_log"

// persistDispatchLog saves the current dispatch log to ADK session state.
func persistDispatchLog(tctx tool.Context, sessionID string) {
	entries := GetDispatchLog(sessionID)
	if len(entries) == 0 {
		return
	}
	durable := make([]durableDispatchEntry, len(entries))
	for i, e := range entries {
		durable[i] = durableDispatchEntry{
			Station:      e.Station,
			Verdict:      int(e.Verdict),
			StartedAt:    e.StartedAt.Unix(),
			DurationMs:   e.Duration.Milliseconds(),
			Seq:          e.Seq,
			ContextUsed:  e.ContextUsed,
			ContextSize:  e.ContextSize,
			ArtifactPath: e.ArtifactPath,
		}
	}
	data, err := json.Marshal(durable)
	if err != nil {
		slog.Warn("Failed to marshal dispatch log", "error", err)
		return
	}
	if setErr := tctx.State().Set(dispatchLogStateKey, string(data)); setErr != nil {
		slog.Warn("Failed to persist dispatch log", "error", setErr)
	}
}

// hydrateDispatchLog restores the dispatch log from ADK session state.
// Running entries from a dead session become VerdictCanceled.
func hydrateDispatchLog(sess adksession.Session, sessionID string) {
	if sess == nil {
		return
	}
	state := sess.State()
	if state == nil {
		return
	}
	raw, err := state.Get(dispatchLogStateKey)
	if err != nil || raw == nil {
		return
	}
	jsonStr, ok := raw.(string)
	if !ok {
		return
	}
	var durable []durableDispatchEntry
	if err := json.Unmarshal([]byte(jsonStr), &durable); err != nil {
		slog.Warn("Failed to unmarshal dispatch log", "error", err)
		return
	}
	entries := make([]DispatchEntry, len(durable))
	for i, d := range durable {
		verdict := DispatchVerdict(d.Verdict)
		if verdict == VerdictRunning {
			verdict = VerdictCanceled // dead session — mark as canceled
		}
		entries[i] = DispatchEntry{
			Station:      d.Station,
			Verdict:      verdict,
			StartedAt:    time.Unix(d.StartedAt, 0),
			Duration:     time.Duration(d.DurationMs) * time.Millisecond,
			Seq:          d.Seq,
			ContextUsed:  d.ContextUsed,
			ContextSize:  d.ContextSize,
			ArtifactPath: d.ArtifactPath,
		}
	}
	SetDispatchLog(sessionID, entries)
}

// getOrStart returns an existing process for the session, or starts a new one.
func (pm *processManager) getOrStart(ctx context.Context, sessionID, prompt, resumeID string) (agentrun.Process, bool, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	key := pm.stateKey(sessionID)

	if proc, ok := pm.processes[sessionID]; ok {
		if proc.Err() == nil {
			return proc, false, nil
		}
		slog.Warn("Station process died, restarting", "station", pm.station, "session", sessionID)
		delete(pm.processes, sessionID)
		updateProcessState(key, ProcessInfo{
			SessionID: sessionID,
			Station:   pm.station,
			Backend:   pm.backend,
			Model:     pm.model,
			State:     ProcessStateError,
			Error:     proc.Err(),
		})
	}

	// Build per-start options: session name + optional resume ID.
	opts := make(map[string]string, len(pm.options)+2)
	for k, v := range pm.options {
		opts[k] = v
	}
	opts[agentrun.OptionSessionName] = pm.station + ":" + sessionID
	if resumeID != "" {
		opts[agentrun.OptionResumeID] = resumeID
		slog.Info("Resuming station session", "station", pm.station, "resume_id", resumeID)
	}

	cwd := pm.resolvedCWDLocked(sessionID)
	slog.Info("Starting station process", "station", pm.station, "session", sessionID, "backend", pm.backend, "model", pm.model, "cwd", cwd)
	proc, err := pm.engine.Start(ctx, agentrun.Session{
		CWD:     cwd,
		Prompt:  prompt,
		Model:   pm.model,
		Options: opts,
		Env:     pm.env,
	})
	if err != nil {
		updateProcessState(key, ProcessInfo{
			SessionID: sessionID,
			Station:   pm.station,
			Backend:   pm.backend,
			Model:     pm.model,
			State:     ProcessStateError,
			Error:     err,
		})
		return nil, false, fmt.Errorf("start station %q process: %w", pm.station, err)
	}

	pm.processes[sessionID] = proc
	updateProcessState(key, ProcessInfo{
		SessionID:   sessionID,
		Station:     pm.station,
		Backend:     pm.backend,
		Model:       pm.model,
		State:       ProcessStateRunning,
		StartedAt:   time.Now(),
		ContextSize: pm.defaultContextWindowSize(),
	})
	return proc, true, nil
}

// defaultContextWindowSize returns the context window for the station.
func (pm *processManager) defaultContextWindowSize() int {
	if pm.recovery != nil && pm.recovery.contextWindow > 0 {
		return pm.recovery.contextWindow
	}
	return defaultContextWindow
}

// contextWindowForStation looks up the context window for a station's model
// from the provider catalog. Returns defaultContextWindow if no match found.
func contextWindowForStation(cfg *config.Config, station config.StationConfig) int {
	if cfg == nil {
		return defaultContextWindow
	}

	backend := station.Backend
	if backend == "" {
		backend = defaultBackend
	}
	providerID := backendToProviderID(backend)

	for _, p := range cfg.KnownProviders() {
		if p.ID != providerID {
			continue
		}
		modelID := station.Model
		if modelID == "" {
			modelID = p.DefaultLargeModelID
		}
		for _, m := range p.Models {
			if m.ID == modelID {
				if m.ContextWindow > 0 {
					return int(m.ContextWindow)
				}
				return defaultContextWindow
			}
		}
	}
	return defaultContextWindow
}

// backendToProviderID maps a station backend string to a provider catalog ID.
func backendToProviderID(backend string) string {
	switch backend {
	case defaultBackend:
		return "anthropic"
	case backendCodex:
		return "openai"
	case backendOpenCode, backendOpenCodeACP:
		return "openai"
	default:
		return backend
	}
}

// killProcess stops the process and removes it from the pool, but leaves
// the buffer intact. Used by RecoveryController during replacement.
func (pm *processManager) killProcess(ctx context.Context, sessionID string) {
	pm.mu.Lock()
	proc, ok := pm.processes[sessionID]
	if ok {
		delete(pm.processes, sessionID)
	}
	pm.mu.Unlock()

	if ok {
		slog.Info("Stopping station process", "station", pm.station, "session", sessionID)
		if err := proc.Stop(ctx); err != nil {
			slog.Warn("Error stopping station process", "station", pm.station, "session", sessionID, "error", err)
		}
	}
	removeProcessState(pm.stateKey(sessionID))
}

// stop terminates the process for a given session.
func (pm *processManager) stop(ctx context.Context, sessionID string) {
	pm.killProcess(ctx, sessionID)
}

// stopAll terminates all processes (app shutdown).
func (pm *processManager) stopAll(ctx context.Context) {
	pm.mu.Lock()
	procs := make(map[string]agentrun.Process, len(pm.processes))
	for k, v := range pm.processes {
		procs[k] = v
	}
	pm.processes = make(map[string]agentrun.Process)
	pm.mu.Unlock()

	for id, proc := range procs {
		slog.Info("Stopping station process", "station", pm.station, "session", id)
		if err := proc.Stop(ctx); err != nil {
			slog.Warn("Error stopping station process", "station", pm.station, "session", id, "error", err)
		}
		removeProcessState(pm.stateKey(id))
	}
}

// publishReplacementActivity publishes an ActivityReplacement to the process state.
func publishReplacementActivity(key, sessionID, _ /* station */, reason string) {
	info, ok := processStates.Get(key)
	if !ok {
		return
	}
	info.Activity = append(info.Activity, ProcessActivity{
		Kind:   ActivityReplacement,
		Name:   "Replaced",
		Detail: fmt.Sprintf("Context exhausted (%s), starting fresh", reason),
	})
	publishActivity(key, sessionID, info)
}

// newStationTool creates an ADK function tool for a named station.
// The sessionID is captured in the closure so the tool knows which process to use.
func newStationTool(pm *processManager, sessionID string, description string,
	perms permission.Service, holdFlag *atomic.Bool, notifier *notify.Notifier,
	turnAbort *atomic.Bool,
) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        pm.station,
		Description: description + stationIsolationFooter,
	}, func(tctx tool.Context, input stationInput) (stationOutput, error) {
		// 0. Input validation — all structured fields are required.
		if err := validateStationInput(input); err != nil {
			return stationOutput{Result: err.Error()}, nil
		}

		// 1a. Route enforcement — returns DENIED as a steering signal so the
		// supervisor can dispatch the prerequisite station. Unlike gate denials,
		// route denials do NOT abort the turn.
		if denial, err := checkRoute(sessionID, pm.station, pm.requires, pm.afterDone); err != nil {
			return stationOutput{}, fmt.Errorf("route check for %q: %w", pm.station, err)
		} else if denial != "" {
			return stationOutput{Result: denial}, nil
		}

		// 1b. Artifact enforcement — requires upstream artifacts in session state.
		if denial, err := checkArtifact(tctx, pm.station, pm.requiresArtifact,
			pm.disableArtifactEnforcement, sessionID); err != nil {
			return stationOutput{}, fmt.Errorf("artifact check for %q: %w", pm.station, err)
		} else if denial != "" {
			return stationOutput{Result: denial}, nil
		}

		// 2. Gate
		approved, err := pm.gate.Check(tctx, sessionID, tctx.FunctionCallID(), input, holdFlag, perms)
		if err != nil {
			return stationOutput{}, fmt.Errorf("gate check for %q: %w", pm.station, err)
		}
		if !approved {
			turnAbort.Store(true)
			return stationOutput{
				Result: fmt.Sprintf("DENIED: User denied %q execution. "+
					"Ask the user why via ask_user, or try a different approach.", pm.station),
				Abort: true,
			}, nil
		}

		// 2. Dispatch
		dispatchIdx := AppendDispatch(sessionID, pm.station)

		// 3. Observe
		uiHandler := pm.observer.Handler(sessionID, func(resumeID string) {
			if resumeID != "" {
				_ = tctx.State().Set(stationResumeStateKey(pm.station), resumeID)
			}
			pm.persist.PersistState(tctx, sessionID)
		})

		// 4. Assemble artifact context for prompt injection.
		var artifactCtx string
		if len(pm.requiresArtifact) > 0 {
			var lines []string
			for _, req := range pm.requiresArtifact {
				if art, _ := getActiveArtifact(tctx, req); art != nil && art.Path != "" {
					lines = append(lines, fmt.Sprintf("Active %s artifact: %s (v%d)", req, art.Path, art.Version))
				}
			}
			if len(lines) > 0 {
				artifactCtx = strings.Join(lines, "\n")
			}
		}

		// 5. Execute with recovery
		var result strings.Builder
		ledger := GetOrCreateLedger(sessionID)
		isError, artifactPath, err := pm.recovery.Run(tctx, tctx, pm, sessionID, input, pm.task, artifactCtx, &result, uiHandler, notifier, ledger)
		if err != nil {
			if tctx.Err() != nil {
				CompleteDispatch(sessionID, dispatchIdx, VerdictCanceled, "")
				pm.stop(context.Background(), sessionID)
			} else {
				CompleteDispatch(sessionID, dispatchIdx, VerdictFailed, "")
			}
			persistDispatchLog(tctx, sessionID)
			return stationOutput{}, fmt.Errorf("station %q: %w", pm.station, err)
		}

		// 5. Finalize
		verdict := VerdictDone
		if isError {
			verdict = VerdictFailed
		}
		resolved := resolveArtifactPath(artifactPath, pm.resolvedCWD(sessionID))
		CompleteDispatch(sessionID, dispatchIdx, verdict, resolved)
		pm.observer.CompleteTurn(sessionID)
		pm.persist.PersistState(tctx, sessionID)
		persistDispatchLog(tctx, sessionID)
		pm.persist.SaveArtifact(tctx, result.String())

		// Write to artifact registry on success with a non-empty path.
		if verdict == VerdictDone && resolved != "" {
			dispatchSeq := getDispatchSeq(sessionID, dispatchIdx)
			setActiveArtifact(tctx, pm.station, resolved, dispatchSeq)
		}

		if isError {
			return stationOutput{Error: result.String()}, nil
		}
		slog.Info("Station artifact path", "station", pm.station, "raw", artifactPath, "resolved", resolved)
		return stationOutput{
			Result:       result.String(),
			ArtifactPath: resolved,
		}, nil
	})
}

// drainFirstTurn drains output from a process whose prompt was baked into
// Start() args. Reads until MessageResult or channel close.
func drainFirstTurn(ctx context.Context, proc agentrun.Process, handler func(agentrun.Message) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-proc.Output():
			if !ok {
				return proc.Err()
			}
			if err := handler(msg); err != nil {
				return err
			}
			if msg.Type == agentrun.MessageResult {
				return nil
			}
		}
	}
}

const maxRepoStateLen = 2000

// captureRepoState runs git status and git diff --stat in the given directory
// and returns a combined summary.
func captureRepoState(ctx context.Context, cwd string) (string, error) {
	var b strings.Builder

	statusCmd := exec.CommandContext(ctx, "git", "-C", cwd, "status", "--porcelain")
	if out, err := statusCmd.Output(); err == nil && len(out) > 0 {
		b.WriteString(string(out))
	}

	diffCmd := exec.CommandContext(ctx, "git", "-C", cwd, "diff", "--stat")
	if out, err := diffCmd.Output(); err == nil && len(out) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(string(out))
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("no git state available")
	}

	result := b.String()
	if len(result) > maxRepoStateLen {
		result = result[:maxRepoStateLen]
	}
	return result, nil
}

// resolveArtifactPath resolves a raw path against the station's CWD.
// Returns the resolved absolute path, or "" if rawPath is empty.
func resolveArtifactPath(rawPath, cwd string) string {
	if rawPath == "" {
		return ""
	}
	if filepath.IsAbs(rawPath) {
		return rawPath
	}
	return filepath.Join(cwd, rawPath)
}

// computeStationCost estimates cost from model pricing when agentrun's
// CostUSD is zero. Claude CLI almost always reports CostUSD natively,
// so this fallback rarely fires. Returns 0 with a warning if no pricing
// data is available.
func computeStationCost(modelID string, u *agentrun.Usage) float64 {
	if u == nil {
		return 0
	}
	slog.Warn("Station CostUSD is zero, no fallback pricing available",
		"model", modelID,
		"input_tokens", u.InputTokens,
		"output_tokens", u.OutputTokens)
	return 0
}
