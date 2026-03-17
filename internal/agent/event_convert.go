package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/message"
)

// eventsToMessages converts persisted ADK events into message.Message objects
// for UI display. This is the reverse of processEvent — it rebuilds the message
// list from stored events for session reload (app restart / session switch).
//
// Only non-partial events are stored by ADK, so this handles final events only.
func eventsToMessages(events adksession.Events, sessionID string) []message.Message {
	b := &replayBuilder{sessionID: sessionID}
	for event := range events.All() {
		if event.Content == nil {
			continue
		}
		if event.Author == "user" {
			b.handleUserEvent(event)
			continue
		}
		if b.handleAgentEvent(event) {
			break // abort detected — stop processing events
		}
	}
	b.finalize()
	return b.result
}

// replayBuilder accumulates message.Message objects from persisted ADK events.
// Extracted from eventsToMessages to keep cognitive complexity manageable.
type replayBuilder struct {
	sessionID        string
	result           []message.Message
	currentAssistant *message.Message
}

func (b *replayBuilder) flushAssistant() {
	if b.currentAssistant != nil {
		b.result = append(b.result, *b.currentAssistant)
		b.currentAssistant = nil
	}
}

func (b *replayBuilder) ensureAssistant(event *adksession.Event) {
	if b.currentAssistant == nil {
		b.currentAssistant = &message.Message{
			ID:        event.ID,
			Role:      message.Assistant,
			SessionID: b.sessionID,
			CreatedAt: event.Timestamp.Unix(),
		}
	}
}

func (b *replayBuilder) handleUserEvent(event *adksession.Event) {
	b.flushAssistant()

	var shellParts []parsedShellEvent
	var textParts []string

	for _, p := range event.Content.Parts {
		if p.Text == "" {
			continue
		}
		if parsed, ok := parseUserShellEvent(p.Text); ok {
			shellParts = append(shellParts, parsed)
		} else {
			textParts = append(textParts, p.Text)
		}
	}

	// Emit standard user message for any non-shell text parts.
	if len(textParts) > 0 {
		userMsg := message.Message{
			ID:        event.ID,
			Role:      message.User,
			SessionID: b.sessionID,
			CreatedAt: event.Timestamp.Unix(),
		}
		for _, t := range textParts {
			userMsg.AppendContent(t)
		}
		userMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		b.result = append(b.result, userMsg)
	}

	// Emit assistant+tool message pairs for each shell event.
	for i, sp := range shellParts {
		toolCallID := fmt.Sprintf("shell-replay-%s-%d", event.ID, i)
		input := marshalBashParams(sp.Command)

		assistantMsg := newInMemoryMessage(b.sessionID, message.Assistant, []message.ContentPart{
			message.ToolCall{
				ID:    toolCallID,
				Name:  tools.BashToolName,
				Input: input,
				State: message.ToolStateDone,
			},
		}, "", "")
		assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		b.result = append(b.result, assistantMsg)

		toolMsg := newInMemoryMessage(b.sessionID, message.Tool, []message.ContentPart{
			message.ToolResult{
				ToolCallID: toolCallID,
				Name:       tools.BashToolName,
				Content:    sp.Output,
				IsError:    sp.ExitCode != 0,
			},
		}, "", "")
		toolMsg.AddFinish(message.FinishReasonEndTurn, "", "")
		b.result = append(b.result, toolMsg)
	}
}

// parsedShellEvent holds data extracted from a <user_shell> XML block.
type parsedShellEvent struct {
	Command  string
	Output   string
	ExitCode int
}

// userShellRe matches <user_shell command="..." exit_code="...">...</user_shell>.
var userShellRe = regexp.MustCompile(`(?s)^<user_shell command="([^"]*)" exit_code="(-?\d+)">\n(.*)\n</user_shell>$`)

// parseUserShellEvent extracts shell event data from <user_shell> XML.
// Returns (parsed, true) if the text is a user_shell event, (zero, false) otherwise.
func parseUserShellEvent(text string) (parsedShellEvent, bool) {
	m := userShellRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return parsedShellEvent{}, false
	}
	exitCode, _ := strconv.Atoi(m[2])
	return parsedShellEvent{
		Command:  m[1],
		Output:   m[3],
		ExitCode: exitCode,
	}, true
}

// handleAgentEvent processes a single agent event. Returns true if an abort
// marker was detected and the caller should stop processing events.
func (b *replayBuilder) handleAgentEvent(event *adksession.Event) bool {
	b.ensureAssistant(event)

	ensure := func() { b.ensureAssistant(event) }
	flush := func() { b.flushAssistant() }
	hasFunctionCall, abortDetected := processEventParts(event, b.sessionID, ensure, flush, &b.currentAssistant, &b.result)

	if abortDetected {
		b.flushAssistant()
		return true
	}

	b.applyGrounding(event)
	b.applyFinishReason(event, hasFunctionCall)
	return false
}

func (b *replayBuilder) applyGrounding(event *adksession.Event) {
	gm := event.GroundingMetadata
	cm := event.CitationMetadata
	if gm == nil && cm == nil {
		return
	}
	hasGrounding := gm != nil && (len(gm.WebSearchQueries) > 0 || len(gm.GroundingChunks) > 0)
	hasCitations := cm != nil && len(cm.Citations) > 0
	if hasGrounding || hasCitations {
		groundingToMessage(gm, cm, b.currentAssistant)
	}
}

func (b *replayBuilder) applyFinishReason(event *adksession.Event, hasFunctionCall bool) {
	if hasFunctionCall || event.FinishReason == "" || event.FinishReason == genai.FinishReasonUnspecified {
		return
	}
	if b.currentAssistant == nil {
		return
	}
	b.currentAssistant.FinishThinking()
	b.currentAssistant.AddFinish(mapFinishReason(event.FinishReason), "", "")
	if event.UsageMetadata != nil {
		u := usageFromMetadata(event.UsageMetadata)
		b.currentAssistant.SetFinishTokens(u.PromptTokens, u.CandidatesTokens, u.TotalTokens)
	}
	b.flushAssistant()
}

func (b *replayBuilder) finalize() {
	if b.currentAssistant != nil && !b.currentAssistant.IsFinished() {
		b.currentAssistant.AddFinish(message.FinishReasonEndTurn, "", "")
	}
	b.flushAssistant()
}

// processEventParts iterates parts within a single ADK event, building assistant
// and tool messages. Returns (hasFunctionCall, abortDetected). Extracted from
// eventsToMessages to reduce cognitive complexity.
func processEventParts(
	event *adksession.Event,
	sessionID string,
	ensureAssistant func(),
	flushAssistant func(),
	currentAssistant **message.Message,
	result *[]message.Message,
) (bool, bool) {
	var hasFunctionCall bool
	for _, p := range event.Content.Parts {
		switch {
		case p.Thought && p.Text != "":
			ensureAssistant()
			(*currentAssistant).AppendReasoningContent(p.Text)

		case p.Text != "":
			ensureAssistant()
			(*currentAssistant).AppendContent(p.Text)

		case p.FunctionCall != nil:
			hasFunctionCall = true
			ensureAssistant()
			input := marshalFunctionCallArgs(p.FunctionCall)
			(*currentAssistant).AddToolCall(message.ToolCall{
				ID:    p.FunctionCall.ID,
				Name:  p.FunctionCall.Name,
				Input: input,
				State: message.ToolStateDone,
			})

		case p.FunctionResponse != nil:
			// Detect dialog cancellation before flushing — the canceled finish
			// reason must be on the assistant message, not the tool result message.
			abort := isAbortResponse(p.FunctionResponse)
			if abort && *currentAssistant != nil {
				(*currentAssistant).FinishThinking()
				(*currentAssistant).AddFinish(message.FinishReasonCanceled, "Dialog canceled by user", "")
			}

			// Tool results are separate messages in Crucible's UI model.
			flushAssistant()

			content, isError := extractFunctionResponseContent(p.FunctionResponse)
			metadata := extractFunctionResponseMetadata(p.FunctionResponse)
			toolMsg := message.Message{
				ID:        event.ID + "-tool",
				Role:      message.Tool,
				SessionID: sessionID,
				Parts: []message.ContentPart{message.ToolResult{
					ToolCallID: p.FunctionResponse.ID,
					Name:       p.FunctionResponse.Name,
					Content:    content,
					Metadata:   metadata,
					IsError:    isError,
				}},
				CreatedAt: event.Timestamp.Unix(),
			}
			toolMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			*result = append(*result, toolMsg)

			if abort {
				return hasFunctionCall, true
			}
		}
	}
	return hasFunctionCall, false
}

// marshalFunctionCallArgs marshals function call args to a JSON string.
func marshalFunctionCallArgs(fc *genai.FunctionCall) string {
	if fc.Args == nil {
		return ""
	}
	b, err := json.Marshal(fc.Args)
	if err != nil {
		return ""
	}
	return string(b)
}

// groundingToMessage adds GroundingContent to the assistant message from persisted metadata.
func groundingToMessage(gm *genai.GroundingMetadata, cm *genai.CitationMetadata, assistant *message.Message) {
	if assistant == nil {
		return
	}
	var gc message.GroundingContent
	if gm != nil {
		gc = groundingFromMetadata(gm)
	}
	gc.Citations = citationsFromMetadata(cm)
	assistant.Parts = append(assistant.Parts, gc)
}

// extractFunctionResponseContent extracts a display string from a function response.
// Returns the content and whether the response represents an error (from the "error" key).
func extractFunctionResponseContent(resp *genai.FunctionResponse) (string, bool) {
	if resp == nil || resp.Response == nil {
		return "", false
	}
	if v, ok := resp.Response["result"]; ok {
		return fmt.Sprint(v), false
	}
	if v, ok := resp.Response["error"]; ok {
		return fmt.Sprint(v), true
	}
	return "", false
}

// isAbortResponse checks whether a FunctionResponse carries a dialog-cancellation
// marker. Station and MCP tools set "_abort": true explicitly. ask_user uses the
// deterministic ErrCanceledMessage error string.
func isAbortResponse(resp *genai.FunctionResponse) bool {
	if resp == nil || resp.Response == nil {
		return false
	}
	// Explicit abort marker (station tools, MCP tools).
	if v, ok := resp.Response["_abort"]; ok {
		if b, ok := v.(bool); ok && b {
			return true
		}
	}
	// ask_user deterministic cancel string.
	if v, ok := resp.Response["error"]; ok {
		if s, ok := v.(string); ok && s == askuser.ErrCanceledMessage {
			return true
		}
	}
	return false
}

// eventsToUserPrompts extracts user prompt texts from ADK events.
// Used for prompt history (up/down arrow navigation).
func eventsToUserPrompts(events adksession.Events, sessionID string) []message.Message {
	var result []message.Message
	for event := range events.All() {
		if event.Author != "user" || event.Content == nil {
			continue
		}
		msg := message.Message{
			ID:        event.ID,
			Role:      message.User,
			SessionID: sessionID,
			CreatedAt: event.Timestamp.Unix(),
		}
		for _, p := range event.Content.Parts {
			if p.Text != "" {
				msg.AppendContent(p.Text)
			}
		}
		// Only include messages that have actual content.
		if msg.Content().Text != "" {
			msg.AddFinish(message.FinishReasonEndTurn, "", "")
			result = append(result, msg)
		}
	}
	return result
}

// newInMemoryMessage creates a message.Message in memory (no DB write) with a generated ID.
func newInMemoryMessage(sessionID string, role message.MessageRole, parts []message.ContentPart, model, provider string) message.Message {
	return message.Message{
		ID:        generateMessageID(),
		Role:      role,
		SessionID: sessionID,
		Parts:     parts,
		Model:     model,
		Provider:  provider,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
}

// generateMessageID creates a unique message ID using UUID to avoid collisions
// under concurrent calls.
func generateMessageID() string {
	return "msg-" + uuid.New().String()
}
