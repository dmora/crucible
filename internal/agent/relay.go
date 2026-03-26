package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// relayExchange records a single operator↔station exchange.
type relayExchange struct {
	Operator      string `json:"operator"`
	StationOutput string `json:"station_output"`
}

// relaySession holds per-session relay state. One per active relay.
// All mutable fields are protected by mu.
type relaySession struct {
	mu         sync.Mutex
	target     string             // station name (immutable after creation)
	exchanges  []relayExchange    // accumulated turns
	resumeID   string             // last resume ID for recovery
	cancelTurn context.CancelFunc // cancels ONLY the current turn's RunDirect
	turnActive bool               // true while RunDirect is executing
	turnDone   chan struct{}      // closed when the current turn completes
	startedAt  time.Time
}

// RelayController manages direct operator-to-station relay sessions.
// State is session-scoped via csync.Map keyed by sessionID.
type RelayController struct {
	sessions        *csync.Map[string, *relaySession]
	stations        map[string]*processManager
	messageBroker   *pubsub.Broker[message.Message]
	activeRequests  *csync.Map[string, *TaskHandle] // shared with sessionAgent
	adkSessionSvc   adksession.Service
	ensureADKSessFn func(ctx context.Context, sessionID string) (adksession.Session, error)
}

// StartRelay begins a relay session targeting the named station.
// Does NOT pre-start the station process — the first SendRelay/RunDirect call
// will spawn it via GetOrStart, matching the standard station lifecycle.
func (rc *RelayController) StartRelay(_ context.Context, sessionID, station string) error {
	if _, ok := rc.stations[station]; !ok {
		return fmt.Errorf("relay: unknown station %q", station)
	}

	// Read existing resume ID from hydrated ProcessInfo so relay
	// continues the station's prior conversation (not a fresh start).
	var existingResumeID string
	key := processStateKey(sessionID, station)
	if info, ok := processStates.Get(key); ok {
		existingResumeID = info.ResumeID
		info.IsRelayDriven = true
		updateProcessState(key, info)
	}

	rc.sessions.Set(sessionID, &relaySession{
		target:    station,
		resumeID:  existingResumeID,
		startedAt: time.Now(),
	})

	slog.Info("Relay started", "session", sessionID, "station", station)
	return nil
}

// SendRelay sends an operator message to the relay target station.
func (rc *RelayController) SendRelay(ctx context.Context, sessionID, msg string) error {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return fmt.Errorf("relay: no active relay for session %q", sessionID)
	}

	target := rs.target
	pm, ok := rc.stations[target]
	if !ok {
		return fmt.Errorf("relay: station %q not found", target)
	}

	// Per-turn cancel context.
	turnCtx, cancelTurn := context.WithCancel(ctx)
	done := make(chan struct{})

	rs.mu.Lock()
	rs.cancelTurn = cancelTurn
	rs.turnActive = true
	rs.turnDone = done
	resumeID := rs.resumeID
	rs.mu.Unlock()

	// Register in activeRequests so IsBusy() returns true.
	relayKey := sessionID + "-relay"
	handle := NewTaskHandle(cancelTurn)
	if prev, swapped := rc.activeRequests.Swap(relayKey, handle); swapped && prev != nil {
		prev.Cancel()
	}

	defer func() {
		cancelTurn()
		rc.activeRequests.DeleteFunc(relayKey, func(h *TaskHandle) bool {
			return h != nil && h.ID == handle.ID
		})
		rs.mu.Lock()
		rs.turnActive = false
		rs.cancelTurn = nil
		rs.turnDone = nil
		rs.mu.Unlock()
		close(done)
	}()

	// Publish pending messages, execute turn, publish result.
	toolCallID, assistantMsg := rc.publishRelayTurnStart(sessionID, target, msg)

	output, resultIsError, runErr := rc.executeRelayTurn(turnCtx, pm, rs, sessionID, msg, resumeID)

	return rc.finalizeRelayTurn(rs, assistantMsg, toolCallID, target, msg, output, resultIsError, runErr)
}

// publishRelayTurnStart publishes the operator message and pending tool call to chat.
// Returns the tool call ID and assistant message for later finalization.
func (rc *RelayController) publishRelayTurnStart(sessionID, target, msg string) (string, message.Message) {
	userMsg := newInMemoryMessage(sessionID, message.User,
		[]message.ContentPart{message.TextContent{Text: msg}}, "", "")
	userMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	rc.messageBroker.Publish(pubsub.CreatedEvent, userMsg.Clone())

	toolCallID := "relay-" + uuid.New().String()
	relayInput, _ := json.Marshal(map[string]string{"message": msg})
	assistantMsg := newInMemoryMessage(sessionID, message.Assistant,
		[]message.ContentPart{
			message.ToolCall{
				ID:    toolCallID,
				Name:  "relay:" + target,
				Input: string(relayInput),
				State: message.ToolStatePending,
			},
		}, "", "")
	assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	rc.messageBroker.Publish(pubsub.CreatedEvent, assistantMsg.Clone())

	return toolCallID, assistantMsg
}

// executeRelayTurn runs the station turn through the recovery pipeline.
// Returns the station output alongside the error/isError flags.
func (rc *RelayController) executeRelayTurn(
	ctx context.Context, pm *processManager, rs *relaySession,
	sessionID, msg, resumeID string,
) (output string, resultIsError bool, runErr error) {
	var result strings.Builder
	ledger := GetOrCreateLedger(sessionID)
	handler := pm.observer.Handler(sessionID, func(newResumeID string) {
		rs.mu.Lock()
		rs.resumeID = newResumeID
		rs.mu.Unlock()
	})

	resultIsError, _, runErr = pm.recovery.RunDirect(
		ctx, pm, sessionID, msg, &result, handler, ledger,
		resumeID, func(id string) {
			rs.mu.Lock()
			rs.resumeID = id
			rs.mu.Unlock()
		},
	)
	return result.String(), resultIsError, runErr
}

// finalizeRelayTurn publishes the tool result and accumulates the exchange.
func (rc *RelayController) finalizeRelayTurn(
	rs *relaySession, assistantMsg message.Message,
	toolCallID, target, msg, output string,
	resultIsError bool, runErr error,
) error {
	canceled := runErr != nil && errors.Is(runErr, context.Canceled)

	assistantMsg.FinishToolCall(toolCallID)
	rc.messageBroker.Publish(pubsub.UpdatedEvent, assistantMsg.Clone())

	toolResult := message.ToolResult{
		ToolCallID: toolCallID,
		Name:       "relay:" + target,
		Content:    output,
		IsError:    resultIsError,
	}
	if canceled {
		toolResult.Content = "Canceled by operator"
		toolResult.IsError = false
	}
	toolResultMsg := newInMemoryMessage(assistantMsg.SessionID, message.Tool,
		[]message.ContentPart{toolResult}, "", "")
	if canceled {
		toolResultMsg.AddFinish(message.FinishReasonCanceled, "Relay turn canceled", "")
	} else {
		toolResultMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	}
	rc.messageBroker.Publish(pubsub.CreatedEvent, toolResultMsg.Clone())

	if !canceled {
		rs.mu.Lock()
		rs.exchanges = append(rs.exchanges, relayExchange{
			Operator:      msg,
			StationOutput: output,
		})
		rs.mu.Unlock()
	}

	if runErr != nil && !canceled {
		return fmt.Errorf("relay turn: %w", runErr)
	}
	return nil
}

// StopRelay ends the relay session and persists exchanges to ADK.
// Waits for any active turn to complete before persisting.
func (rc *RelayController) StopRelay(ctx context.Context, sessionID string) error {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return nil // no-op if not relaying
	}

	rc.waitForTurn(rs)

	rs.mu.Lock()
	exchanges := rs.exchanges
	target := rs.target
	rs.mu.Unlock()

	rc.persistRelayEvent(ctx, sessionID, rs)

	// Clear relay-driven flag on ProcessInfo.
	key := processStateKey(sessionID, target)
	if info, ok := processStates.Get(key); ok {
		info.IsRelayDriven = false
		updateProcessState(key, info)
	}

	rc.sessions.Del(sessionID)
	slog.Info("Relay stopped", "session", sessionID, "station", target,
		"exchanges", len(exchanges))
	return nil
}

// SwitchRelay switches from the current relay target to a new station.
// Cancels any active turn, waits for it to drain, persists, then starts on the new station.
func (rc *RelayController) SwitchRelay(ctx context.Context, sessionID, newStation string) error {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return rc.StartRelay(ctx, sessionID, newStation)
	}

	// Cancel active turn if running and wait for it to drain.
	rc.cancelAndWait(rs)

	// Persist old relay exchanges.
	rc.persistRelayEvent(ctx, sessionID, rs)

	// Clear relay-driven flag on old station.
	oldKey := processStateKey(sessionID, rs.target)
	if info, ok := processStates.Get(oldKey); ok {
		info.IsRelayDriven = false
		updateProcessState(oldKey, info)
	}

	rc.sessions.Del(sessionID)

	slog.Info("Relay switching", "session", sessionID,
		"from", rs.target, "to", newStation)
	return rc.StartRelay(ctx, sessionID, newStation)
}

// CancelRelayTurn cancels the current relay turn without exiting relay mode.
func (rc *RelayController) CancelRelayTurn(sessionID string) {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return
	}
	rs.mu.Lock()
	cancel := rs.cancelTurn
	rs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// RelayTarget returns the current relay station name, or nil if not relaying.
func (rc *RelayController) RelayTarget(sessionID string) *string {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return nil
	}
	return &rs.target
}

// IsRelayActive reports whether relay mode is active for the session.
func (rc *RelayController) IsRelayActive(sessionID string) bool {
	_, ok := rc.sessions.Get(sessionID)
	return ok
}

// IsRelayTurnBusy reports whether a relay turn is currently executing.
func (rc *RelayController) IsRelayTurnBusy(sessionID string) bool {
	rs, ok := rc.sessions.Get(sessionID)
	if !ok {
		return false
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.turnActive
}

// waitForTurn waits for an active turn to complete. No-op if idle.
func (rc *RelayController) waitForTurn(rs *relaySession) {
	rs.mu.Lock()
	done := rs.turnDone
	rs.mu.Unlock()
	if done != nil {
		<-done
	}
}

// cancelAndWait cancels any active turn and waits for it to drain.
func (rc *RelayController) cancelAndWait(rs *relaySession) {
	rs.mu.Lock()
	cancel := rs.cancelTurn
	done := rs.turnDone
	rs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// --- Persistence ---

const (
	relayLogStateKey = "relay:log"
)

// relayLogSegment records one completed relay session (StartRelay → StopRelay).
type relayLogSegment struct {
	Station   string          `json:"station"`
	StartedAt int64           `json:"started_at"`
	Duration  string          `json:"duration"`
	Exchanges []relayExchange `json:"exchanges"`
}

// persistRelayEvent writes the relay session to both ADK state (full log) and
// ADK event (full transcript for supervisor). Caller must ensure no concurrent
// turn is running (call waitForTurn first).
func (rc *RelayController) persistRelayEvent(ctx context.Context, sessionID string, rs *relaySession) {
	rs.mu.Lock()
	exchanges := make([]relayExchange, len(rs.exchanges))
	copy(exchanges, rs.exchanges)
	target := rs.target
	startedAt := rs.startedAt
	rs.mu.Unlock()

	if len(exchanges) == 0 {
		return
	}

	adkSess, err := rc.ensureADKSessFn(ctx, sessionID)
	if err != nil {
		slog.Error("Relay persist: failed to get ADK session", "err", err)
		return
	}
	duration := time.Since(startedAt)

	// 1. Append segment to relay log in ADK session STATE (not visible to supervisor).
	segment := relayLogSegment{
		Station:   target,
		StartedAt: startedAt.Unix(),
		Duration:  formatDuration(duration),
		Exchanges: exchanges,
	}
	if err := appendRelayLogSegment(adkSess, segment); err != nil {
		slog.Error("Relay persist: failed to append log segment", "err", err,
			"session", sessionID, "station", target)
	}

	// 2. Persist full transcript as ADK EVENT (supervisor sees this).
	body := formatRelayTranscript(exchanges)
	xmlText := fmt.Sprintf("<user_relay station=%q turns=\"%d\" duration=%q>\n%s\n</user_relay>",
		target, len(exchanges), formatDuration(duration), body)

	event := adksession.NewEvent("")
	event.Author = "user"
	event.Content = &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: xmlText}},
	}
	if err := rc.adkSessionSvc.AppendEvent(ctx, adkSess, event); err != nil {
		slog.Error("Relay persist: failed to append event", "err", err)
	}

	slog.Info("Relay persisted", "session", sessionID, "station", target,
		"exchanges", len(exchanges), "duration", formatDuration(duration))
}

// appendRelayLogSegment appends a segment to the relay log in ADK session state.
func appendRelayLogSegment(sess adksession.Session, segment relayLogSegment) error {
	log := make([]relayLogSegment, 0, 1)
	if raw, err := sess.State().Get(relayLogStateKey); err == nil && raw != nil {
		if s, ok := raw.(string); ok {
			if err := json.Unmarshal([]byte(s), &log); err != nil {
				return fmt.Errorf("unmarshal relay log: %w", err)
			}
		}
	}
	log = append(log, segment)
	data, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("marshal relay log: %w", err)
	}
	if err := sess.State().Set(relayLogStateKey, string(data)); err != nil {
		return fmt.Errorf("set relay log state: %w", err)
	}
	return nil
}

// formatRelayTranscript formats relay exchanges for the supervisor event.
// Includes the full transcript without truncation or summarization.
func formatRelayTranscript(exchanges []relayExchange) string {
	var b strings.Builder
	for i, ex := range exchanges {
		if i > 0 {
			b.WriteString("\n\n")
		}

		// Prevent XML injection.
		operator := html.EscapeString(ex.Operator)
		stationOutput := html.EscapeString(ex.StationOutput)

		fmt.Fprintf(&b, "Operator: %s\n", operator)
		fmt.Fprintf(&b, "Station: %s", stationOutput)
	}
	return b.String()
}

// formatDuration returns a compact elapsed string like "0:42" or "12:05".
// Duplicated from ui/common to avoid agent→ui import cycle.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}
