package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/pubsub"
	adksession "google.golang.org/adk/session"
)

// ProcessState represents the current state of an agentrun process.
type ProcessState int

const (
	ProcessStateIdle    ProcessState = iota // No process for this session
	ProcessStateRunning                     // Process alive
	ProcessStateError                       // Process died or failed to start
	ProcessStateStopped                     // Explicitly stopped
)

func (s ProcessState) String() string {
	switch s {
	case ProcessStateIdle:
		return "idle"
	case ProcessStateRunning:
		return "running"
	case ProcessStateError:
		return "error"
	case ProcessStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// maxProcessActivity is the maximum number of activity entries to keep.
// High enough to preserve full history for scrollback while bounding memory.
const maxProcessActivity = 200

// ActivityKind classifies an activity entry for rendering.
type ActivityKind uint

const (
	ActivityTool        ActivityKind = iota // normal tool call
	ActivityError                           // tool or runtime error
	ActivityThinking                        // reasoning/thinking block
	ActivityReplacement                     // station replaced due to context exhaustion
)

// ProcessActivity represents a single sub-agent action for UI display.
// For ActivityError, Name is the error code (or "Error" when absent) and
// Detail holds the truncated error message from the CLI.
type ProcessActivity struct {
	Kind   ActivityKind // how to render this entry
	Name   string       // tool name: "Read", "Bash", "Write", etc.
	Detail string       // compact detail: file path, command, etc.
}

// ProcessPhase describes what the sub-agent is doing right now (for spinner).
type ProcessPhase string

const (
	PhaseIdle       ProcessPhase = ""
	PhaseThinking   ProcessPhase = "Thinking"
	PhaseGenerating ProcessPhase = "Generating"
)

// OperatorState represents the operator-facing state of a station process.
// Used by both chat card and sidebar for semantic state display.
type OperatorState string

const (
	OpStateIdle              OperatorState = "Idle"
	OpStateThinking          OperatorState = "Thinking"
	OpStateReading           OperatorState = "Reading"
	OpStateEditing           OperatorState = "Editing"
	OpStateTesting           OperatorState = "Testing"
	OpStateRunning           OperatorState = "Running"
	OpStateSearching         OperatorState = "Searching"
	OpStateDone              OperatorState = "Done"
	OpStateFailed            OperatorState = "Failed"
	OpStateCanceled          OperatorState = "Canceled"
	OpStateWaitingPermission OperatorState = "Waiting for permission"
	OpStateDirect            OperatorState = "Direct"
)

// ProcessEventType classifies process events.
type ProcessEventType uint

const (
	ProcessEventStateChanged   ProcessEventType = iota
	ProcessEventActivityUpdate                  // sub-agent tool activity changed
	ProcessEventDispatchUpdate                  // dispatch log changed
	ProcessEventRetry                           // supervisor tool retry in progress
	ProcessEventRetryExhausted                  // supervisor tool retries exhausted
)

// ProcessEvent is published when a process state changes.
type ProcessEvent struct {
	Type      ProcessEventType
	SessionID string
	Station   string // station name (e.g. "plan", "inspect")
	State     ProcessState

	// Retry metadata (only for ProcessEventRetry / ProcessEventRetryExhausted).
	RetryTool    string // tool name that failed
	RetryAttempt int    // current attempt (1-based)
	RetryMax     int    // max retry attempts (from observer config)
	RetryError   string // truncated error excerpt (from error_details)
}

// ProcessInfo holds information about an agentrun process for UI display.
type ProcessInfo struct {
	SessionID string
	Station   string // station name (e.g. "plan", "inspect", "fabricate")
	Backend   string // "claude", "codex", "opencode"
	Model     string // resolved model name from CLI init
	State     ProcessState
	Error     error
	PID       int
	StartedAt time.Time

	// ResumeID is the CLI session/thread ID for resuming across restarts.
	// Captured from MessageInit.ResumeID.
	ResumeID string

	// Context window fuel gauge (cumulative across turns).
	ContextUsed int // tokens consumed
	ContextSize int // total capacity (0 = unknown)

	// Generation tracks how many times this station has been replaced
	// due to context exhaustion (0 = original, 1 = first replacement, ...).
	Generation int

	// Recent sub-agent tool activity (capped to maxProcessActivity).
	Activity []ProcessActivity

	// Phase is the sub-agent's current phase (for spinner text).
	Phase ProcessPhase

	// IsRelayDriven is true when the operator is driving this station via relay mode.
	IsRelayDriven bool
}

var (
	processStates = csync.NewMap[string, ProcessInfo]()
	processBroker = pubsub.NewBroker[ProcessEvent]()
)

// processStateKey returns the composite key for processStates: "sessionID:station".
func processStateKey(sessionID, station string) string {
	return sessionID + ":" + station
}

// SubscribeProcessEvents returns a channel for process state change events.
func SubscribeProcessEvents(ctx context.Context) <-chan pubsub.Event[ProcessEvent] {
	return processBroker.Subscribe(ctx)
}

// GetProcessStates returns the current state of all agentrun processes.
func GetProcessStates() map[string]ProcessInfo {
	return processStates.Copy()
}

// ShutdownProcessBroker shuts down the process event broker.
func ShutdownProcessBroker() {
	processBroker.Shutdown()
}

// updateProcessState updates the state and publishes an event.
// The key is "sessionID:station" for multi-station support.
func updateProcessState(key string, info ProcessInfo) {
	processStates.Set(key, info)
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:      ProcessEventStateChanged,
		SessionID: info.SessionID,
		Station:   info.Station,
		State:     info.State,
	})
}

// removeProcessState removes a process entry and publishes a stopped event.
// The key is "sessionID:station".
func removeProcessState(key string) {
	info, ok := processStates.Get(key)
	processStates.Del(key)

	var sessionID, station string
	if ok {
		sessionID = info.SessionID
		station = info.Station
	}
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:      ProcessEventStateChanged,
		SessionID: sessionID,
		Station:   station,
		State:     ProcessStateStopped,
	})
}

// HydrateSessionProcessStates restores persisted station state from an ADK
// session into the in-memory processStates map. Entries are created with
// ProcessStateStopped. Live (running) entries are never overwritten.
// A nil session or nil state is a no-op (returns nil).
func HydrateSessionProcessStates(
	sess adksession.Session,
	sessionID string,
	stations map[string]config.StationConfig,
) error {
	if sess == nil {
		return nil
	}
	state := sess.State()
	if state == nil {
		return nil
	}

	// Clear stale (non-running) entries for this session before hydrating,
	// so a hydration failure leaves "no station state" rather than stale cache.
	// Running processes are preserved.
	for key, info := range processStates.Copy() {
		if info.SessionID == sessionID && info.State != ProcessStateRunning {
			processStates.Del(key)
		}
	}

	// Hydrate dispatch log from ADK session state.
	hydrateDispatchLog(sess, sessionID)

	for name, cfg := range stations {
		key := sessionID + ":" + name

		// Don't overwrite a live process.
		if existing, ok := processStates.Get(key); ok && existing.State == ProcessStateRunning {
			continue
		}

		raw, err := state.Get(stationStateKey(name))
		if err != nil || raw == nil {
			continue
		}
		jsonStr, ok := raw.(string)
		if !ok {
			continue
		}

		var ds stationDurableState
		if err := json.Unmarshal([]byte(jsonStr), &ds); err != nil {
			slog.Warn("Failed to unmarshal station durable state", "station", name, "error", err)
			continue
		}

		info := ProcessInfo{
			SessionID:   sessionID,
			Station:     name,
			Backend:     cmpOr(ds.Backend, cfg.Backend),
			Model:       ds.Model,
			State:       ProcessStateStopped,
			ResumeID:    ds.ResumeID,
			ContextUsed: ds.ContextUsed,
			ContextSize: ds.ContextSize,
		}
		if ds.StartedAt > 0 {
			info.StartedAt = time.Unix(ds.StartedAt, 0)
		}
		updateProcessState(key, info)
	}
	return nil
}

// cmpOr returns a if non-empty, else b.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// PurgeSessionProcessStates removes all process state entries for a session
// and publishes stopped events so the UI updates.
func PurgeSessionProcessStates(sessionID string) {
	all := processStates.Copy()
	for key, info := range all {
		if info.SessionID == sessionID {
			removeProcessState(key)
		}
	}
	purgeSessionDispatchLog(sessionID)
	PurgeLedger(sessionID)
}

// ---------------------------------------------------------------------------
// Dispatch log — chronological history of station invocations per session.
// ---------------------------------------------------------------------------

// DispatchVerdict represents the outcome of a station dispatch.
type DispatchVerdict int

const (
	VerdictRunning  DispatchVerdict = iota
	VerdictDone                     // station completed successfully
	VerdictFailed                   // station returned an error
	VerdictCanceled                 // station was canceled (user or app restart)
)

// DispatchEntry records a single station invocation in the dispatch log.
type DispatchEntry struct {
	Station      string
	Verdict      DispatchVerdict
	StartedAt    time.Time
	Duration     time.Duration // zero while running
	Seq          int           // monotonic within session
	ContextUsed  int           // context tokens at completion (0 = unknown)
	ContextSize  int           // context window capacity (0 = unknown)
	ArtifactPath string        // path to primary artifact (empty if none)
}

// dispatchLog is the per-session append-only dispatch history.
type dispatchLog struct {
	mu      sync.Mutex
	entries []DispatchEntry
	nextSeq int
}

var dispatchLogs = csync.NewMap[string, *dispatchLog]()

// getOrCreateLog returns the dispatch log for a session, creating it if needed.
func getOrCreateLog(sessionID string) *dispatchLog {
	if dl, ok := dispatchLogs.Get(sessionID); ok {
		return dl
	}
	dl := &dispatchLog{}
	dispatchLogs.Set(sessionID, dl)
	return dl
}

// AppendDispatch records a new VerdictRunning entry and publishes an event.
// Returns the entry index for later completion via CompleteDispatch.
func AppendDispatch(sessionID, station string) int {
	dl := getOrCreateLog(sessionID)
	dl.mu.Lock()
	idx := len(dl.entries)
	dl.entries = append(dl.entries, DispatchEntry{
		Station:   station,
		Verdict:   VerdictRunning,
		StartedAt: time.Now(),
		Seq:       dl.nextSeq,
	})
	dl.nextSeq++
	dl.mu.Unlock()

	publishDispatchUpdate(sessionID, station)
	return idx
}

// CompleteDispatch sets the verdict, duration, and artifact path on a previously appended entry.
func CompleteDispatch(sessionID string, index int, verdict DispatchVerdict, artifactPath string) {
	dl, ok := dispatchLogs.Get(sessionID)
	if !ok {
		return
	}
	dl.mu.Lock()
	if index >= 0 && index < len(dl.entries) {
		dl.entries[index].Verdict = verdict
		dl.entries[index].Duration = time.Since(dl.entries[index].StartedAt)
		dl.entries[index].ArtifactPath = artifactPath
		// Snapshot context usage from the live process state.
		key := sessionID + ":" + dl.entries[index].Station
		if info, ok := processStates.Get(key); ok {
			dl.entries[index].ContextUsed = info.ContextUsed
			dl.entries[index].ContextSize = info.ContextSize
		}
	}
	dl.mu.Unlock()

	publishDispatchUpdate(sessionID, "")
}

// GetDispatchLog returns a copy of the dispatch log for a session.
func GetDispatchLog(sessionID string) []DispatchEntry {
	dl, ok := dispatchLogs.Get(sessionID)
	if !ok {
		return nil
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	out := make([]DispatchEntry, len(dl.entries))
	copy(out, dl.entries)
	return out
}

// SetDispatchLog replaces the dispatch log for a session (used during hydration).
func SetDispatchLog(sessionID string, entries []DispatchEntry) {
	dl := getOrCreateLog(sessionID)
	dl.mu.Lock()
	dl.entries = entries
	if len(entries) > 0 {
		dl.nextSeq = entries[len(entries)-1].Seq + 1
	}
	dl.mu.Unlock()
}

// getDispatchSeq returns the monotonic Seq for the dispatch entry at index,
// or -1 if the index is out of bounds. Used by the artifact registry to
// record the logical sequence number, not the slice position.
func getDispatchSeq(sessionID string, index int) int {
	dl, ok := dispatchLogs.Get(sessionID)
	if !ok {
		return -1
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if index < 0 || index >= len(dl.entries) {
		return -1
	}
	return dl.entries[index].Seq
}

// purgeSessionDispatchLog removes the dispatch log for a session.
func purgeSessionDispatchLog(sessionID string) {
	dispatchLogs.Del(sessionID)
}

// publishDispatchUpdate publishes a dispatch log change event.
func publishDispatchUpdate(sessionID, station string) {
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:      ProcessEventDispatchUpdate,
		SessionID: sessionID,
		Station:   station,
	})
}
