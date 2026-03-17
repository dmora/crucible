package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dmora/agentrun"
	"github.com/dmora/crucible/internal/pubsub"
)

// StationObserver translates station message streams into UI-visible state.
// Updates ProcessInfo and publishes events via processBroker.
type StationObserver struct {
	station string
}

// Handler returns a message handler function that updates ProcessInfo
// for the given session. persistFn is called on MessageInit with the
// resume ID so the caller can persist it to ADK session state.
func (so *StationObserver) Handler(sessionID string, persistFn func(resumeID string)) func(agentrun.Message) error {
	return func(msg agentrun.Message) error {
		switch msg.Type {
		case agentrun.MessageText:
			so.handleUsage(sessionID, msg)
			if msg.Tool != nil {
				so.handleToolActivity(sessionID, msg)
			} else {
				so.setPhase(sessionID, PhaseGenerating)
			}
		case agentrun.MessageThinking:
			so.handleThinking(sessionID, msg)
		case agentrun.MessageThinkingDelta:
			so.setPhase(sessionID, PhaseThinking)
		case agentrun.MessageTextDelta:
			so.setPhase(sessionID, PhaseGenerating)
		case agentrun.MessageError:
			slog.Warn("Station error", "station", so.station, "session", sessionID, "error", msg.Content, "code", msg.ErrorCode)
			so.handleError(sessionID, msg)
		case agentrun.MessageInit:
			so.handleInit(sessionID, msg)
			if persistFn != nil {
				persistFn(msg.ResumeID)
			}
		case agentrun.MessageContextWindow:
			so.handleUsage(sessionID, msg)
		case agentrun.MessageResult:
			so.handleUsage(sessionID, msg)
		case agentrun.MessageToolUse:
			so.handleToolActivity(sessionID, msg)
		}
		return nil
	}
}

// CompleteTurn resets phase to idle after a successful turn.
func (so *StationObserver) CompleteTurn(sessionID string) {
	so.setPhase(sessionID, PhaseIdle)
}

// ClearActivity resets the activity log for a new tool invocation.
func (so *StationObserver) ClearActivity(sessionID string) {
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}
	info.Activity = nil
	updateProcessState(key, info)
}

// handleInit captures model name, PID, and resume ID from the init handshake.
func (so *StationObserver) handleInit(sessionID string, msg agentrun.Message) {
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}
	if msg.Init != nil && msg.Init.Model != "" {
		info.Model = msg.Init.Model
	}
	if msg.Process != nil && msg.Process.PID > 0 {
		info.PID = msg.Process.PID
	}
	if msg.ResumeID != "" {
		info.ResumeID = msg.ResumeID
	}
	updateProcessState(key, info)
}

// handleToolActivity appends a sub-agent tool call to the activity log.
func (so *StationObserver) handleToolActivity(sessionID string, msg agentrun.Message) {
	if msg.Tool == nil {
		return
	}
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}

	detail := toolActivityDetail(msg.Tool.Name, msg.Tool.Input)
	info.Activity = append(info.Activity, ProcessActivity{
		Kind:   ActivityTool,
		Name:   msg.Tool.Name,
		Detail: detail,
	})
	if len(info.Activity) > maxProcessActivity {
		info.Activity = info.Activity[len(info.Activity)-maxProcessActivity:]
	}
	info.Phase = PhaseIdle

	publishActivity(key, sessionID, info)
}

// handleError appends an error entry to the activity log.
func (so *StationObserver) handleError(sessionID string, msg agentrun.Message) {
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}

	detail := msg.Content
	if len(detail) > 80 {
		detail = detail[:77] + "..."
	}

	name := "Error"
	if msg.ErrorCode != "" {
		name = msg.ErrorCode
	}

	info.Activity = append(info.Activity, ProcessActivity{
		Kind:   ActivityError,
		Name:   name,
		Detail: detail,
	})
	if len(info.Activity) > maxProcessActivity {
		info.Activity = info.Activity[len(info.Activity)-maxProcessActivity:]
	}

	publishActivity(key, sessionID, info)
}

// handleThinking adds or replaces a thinking entry in the activity log.
func (so *StationObserver) handleThinking(sessionID string, msg agentrun.Message) {
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}

	detail := strings.ReplaceAll(msg.Content, "\n", " ")

	entry := ProcessActivity{
		Kind:   ActivityThinking,
		Name:   "Thinking",
		Detail: detail,
	}

	if n := len(info.Activity); n > 0 && info.Activity[n-1].Kind == ActivityThinking {
		info.Activity[n-1] = entry
	} else {
		info.Activity = append(info.Activity, entry)
		if len(info.Activity) > maxProcessActivity {
			info.Activity = info.Activity[len(info.Activity)-maxProcessActivity:]
		}
	}
	info.Phase = PhaseThinking

	publishActivity(key, sessionID, info)
}

// setPhase updates the current phase (what the sub-agent is doing right now).
func (so *StationObserver) setPhase(sessionID string, phase ProcessPhase) {
	key := processStateKey(sessionID, so.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}
	if info.Phase == phase {
		return
	}
	info.Phase = phase
	publishActivity(key, sessionID, info)
}

// handleUsage updates context usage from any message carrying context fill data.
func (so *StationObserver) handleUsage(sessionID string, msg agentrun.Message) {
	used, size, ok := agentrun.ContextFill(msg)
	if !ok {
		return
	}
	key := processStateKey(sessionID, so.station)
	info, has := processStates.Get(key)
	if !has {
		return
	}
	if used > info.ContextUsed {
		info.ContextUsed = used
	}
	if size > 0 {
		info.ContextSize = size
	}
	updateProcessState(key, info)
}

// publishActivity stores updated info and publishes an activity event.
// Package-level helper used by the observer.
func publishActivity(key, sessionID string, info ProcessInfo) {
	processStates.Set(key, info)
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:      ProcessEventActivityUpdate,
		SessionID: sessionID,
		Station:   info.Station,
		State:     info.State,
	})
}

// toolActivityDetail extracts a compact detail string from tool input JSON.
// Package-level helper used by the observer.
func toolActivityDetail(_ string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return ""
	}
	pathKeys := map[string]bool{"file_path": true, "path": true, "url": true}
	for _, key := range []string{"command", "file_path", "path", "pattern", "query", "url", "task", "skill", "subagent_type", "description"} {
		v, ok := params[key]
		if !ok {
			continue
		}
		s := fmt.Sprint(v)
		if len(s) > 60 {
			if pathKeys[key] {
				s = "…" + s[len(s)-59:]
			} else {
				s = s[:57] + "..."
			}
		}
		return s
	}
	return ""
}
