package agent

import (
	"encoding/json"
	"log/slog"

	adksession "google.golang.org/adk/session"

	"github.com/dmora/crucible/internal/message"
)

// relayLogToMessages reads the relay log from ADK session state and
// converts each segment's exchanges into message.Message objects
// (user message + synthetic relay:<station> tool call per exchange).
func relayLogToMessages(sess adksession.Session, sessionID string) []message.Message {
	state := sess.State()
	if state == nil {
		return nil
	}
	raw, err := state.Get(relayLogStateKey)
	if err != nil || raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}

	var log []relayLogSegment
	if err := json.Unmarshal([]byte(s), &log); err != nil {
		slog.Error("Failed to unmarshal relay log from ADK state",
			"err", err, "session", sessionID)
		return nil
	}

	var msgs []message.Message
	for _, seg := range log {
		segMsgs := segmentToMessages(seg, sessionID)
		msgs = append(msgs, segMsgs...)
	}
	return msgs
}

// segmentToMessages converts a single relay log segment into message pairs.
// Returns nil if the segment has no station name (corrupt data guard).
func segmentToMessages(seg relayLogSegment, sessionID string) []message.Message {
	if seg.Station == "" {
		return nil
	}
	var msgs []message.Message
	for i, ex := range seg.Exchanges {
		// Operator message.
		userMsg := newInMemoryMessage(sessionID, message.User,
			[]message.ContentPart{message.TextContent{Text: ex.Operator}}, "", "")
		userMsg.CreatedAt = seg.StartedAt + int64(i)*2 // monotonic ordering
		userMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		msgs = append(msgs, userMsg)

		// Relay turn (synthetic tool call).
		toolCallID := "relay-replay-" + seg.Station + "-" + userMsg.ID
		relayInput, _ := json.Marshal(map[string]string{"message": ex.Operator})
		assistantMsg := newInMemoryMessage(sessionID, message.Assistant,
			[]message.ContentPart{
				message.ToolCall{
					ID:    toolCallID,
					Name:  "relay:" + seg.Station,
					Input: string(relayInput),
					State: message.ToolStateDone,
				},
			}, "", "")
		assistantMsg.CreatedAt = seg.StartedAt + int64(i)*2 + 1
		assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		msgs = append(msgs, assistantMsg)

		// Tool result.
		toolMsg := newInMemoryMessage(sessionID, message.Tool,
			[]message.ContentPart{
				message.ToolResult{
					ToolCallID: toolCallID,
					Name:       "relay:" + seg.Station,
					Content:    ex.StationOutput,
				},
			}, "", "")
		toolMsg.CreatedAt = seg.StartedAt + int64(i)*2 + 1
		toolMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		msgs = append(msgs, toolMsg)
	}
	return msgs
}

// mergeByTimestamp merges two sorted-by-CreatedAt message lists.
func mergeByTimestamp(a, b []message.Message) []message.Message {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}

	result := make([]message.Message, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].CreatedAt <= b[j].CreatedAt {
			result = append(result, a[i])
			i++
		} else {
			result = append(result, b[j])
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}
