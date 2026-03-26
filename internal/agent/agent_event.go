package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
)

// eventProcessor handles ADK events for a single agent turn.
// Created at the start of the event loop in Run(), used for one iteration.
type eventProcessor struct {
	broker         *pubsub.Broker[message.Message]
	metrics        *csync.Map[string, *TurnMetrics]
	msg            *message.Message
	result         *AgentResult
	ld             *loopDetector
	throttle       *streamThrottle
	sessions       session.Service               // for todos bridge
	lastTodoUpdate *csync.Map[string, time.Time] // per-session staleness tracking
	turnAbort      *atomic.Bool                  // set by tool on dialog cancel; checked after FunctionResponse
}

func newEventProcessor(
	broker *pubsub.Broker[message.Message],
	metrics *csync.Map[string, *TurnMetrics],
	msg *message.Message,
	result *AgentResult,
	sessions session.Service,
	lastTodoUpdate *csync.Map[string, time.Time],
	turnAbort *atomic.Bool,
) *eventProcessor {
	return &eventProcessor{
		broker:         broker,
		metrics:        metrics,
		msg:            msg,
		result:         result,
		ld:             &loopDetector{},
		throttle:       newStreamThrottle(broker),
		sessions:       sessions,
		lastTodoUpdate: lastTodoUpdate,
		turnAbort:      turnAbort,
	}
}

// process handles a single ADK event, returning true if the caller should stop.
func (ep *eventProcessor) process(event *adksession.Event) bool {
	ep.handleUsage(event)
	ep.handleGrounding(event)

	var hasFunctionCall bool
	if event.Content != nil {
		for _, p := range event.Content.Parts {
			switch {
			case p.Thought && p.Text != "" && event.Partial:
				ep.handleThinking(p)
			case p.Text != "" && event.Partial:
				ep.handleText(p)
			case p.FunctionCall != nil:
				hasFunctionCall = true
				if ep.handleFunctionCall(p) {
					return true // loop detected
				}
			case p.FunctionResponse != nil:
				if ep.handleFunctionResponse(p) {
					return true // dialog canceled — stop turn
				}
			}
		}
	}

	ep.handleFinish(event, hasFunctionCall)
	return false
}

func (ep *eventProcessor) handleUsage(event *adksession.Event) {
	if event.UsageMetadata == nil {
		return
	}
	ep.result.TotalUsage = usageFromMetadata(event.UsageMetadata)
	if tm, ok := ep.metrics.Get(ep.msg.SessionID); ok {
		tm.Usage = ep.result.TotalUsage
	}
}

func (ep *eventProcessor) handleGrounding(event *adksession.Event) {
	if event.GroundingMetadata == nil && event.CitationMetadata == nil {
		return
	}
	if event.GroundingMetadata != nil {
		slog.Debug("Grounding metadata received",
			"queries", len(event.GroundingMetadata.WebSearchQueries),
			"chunks", len(event.GroundingMetadata.GroundingChunks),
			"partial", event.Partial,
		)
	}
	ep.publishGrounding(event.GroundingMetadata, event.CitationMetadata)
}

func (ep *eventProcessor) handleThinking(p *genai.Part) {
	ep.msg.AppendReasoningContent(p.Text)
	ep.throttle.publish(ep.msg)
	if tm, ok := ep.metrics.Get(ep.msg.SessionID); ok {
		tm.StreamedBytes += int64(len(p.Text))
	}
}

func (ep *eventProcessor) handleText(p *genai.Part) {
	text := p.Text
	// Strip leading newline from initial text.
	if len(ep.msg.Parts) == 0 {
		text = strings.TrimPrefix(text, "\n")
	}
	ep.msg.AppendContent(text)
	ep.throttle.publish(ep.msg)
	if tm, ok := ep.metrics.Get(ep.msg.SessionID); ok {
		tm.StreamedBytes += int64(len(p.Text))
	}
}

// handleFunctionCall processes a function call part. Returns true if loop detected.
func (ep *eventProcessor) handleFunctionCall(p *genai.Part) bool {
	var input string
	if p.FunctionCall.Args != nil {
		if b, err := json.Marshal(p.FunctionCall.Args); err == nil {
			input = string(b)
		}
	}
	ep.msg.AddToolCall(message.ToolCall{
		ID:    p.FunctionCall.ID,
		Name:  p.FunctionCall.Name,
		Input: input,
		State: message.ToolStatePending,
	})
	ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())
	if ep.ld.track(p.FunctionCall.Name) {
		slog.Warn("Tool call loop detected, stopping agent",
			"tool", p.FunctionCall.Name,
			"session_id", ep.msg.SessionID,
			"consecutive", ep.ld.count)
		detail := fmt.Sprintf("Tool %q called %d times consecutively", p.FunctionCall.Name, ep.ld.count)
		ep.msg.AddFinish(message.FinishReasonError, "Tool loop detected", detail)
		ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())
		return true
	}
	return false
}

// handleFunctionResponse processes a function response part.
// Returns true if the tool signaled that the turn should stop (dialog canceled).
func (ep *eventProcessor) handleFunctionResponse(p *genai.Part) bool {
	// Mark the tool call as finished so the spinner stops.
	ep.msg.FinishToolCall(p.FunctionResponse.ID)
	ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())

	content, isError := extractFunctionResponseContent(p.FunctionResponse)
	metadata := extractFunctionResponseMetadata(p.FunctionResponse)

	// Bridge: persist todos on successful tool result.
	if p.FunctionResponse.Name == tools.TodosToolName && !isError {
		ep.bridgeTodos(metadata)
	}

	artifactPath := extractFunctionResponseArtifactPath(p.FunctionResponse)
	toolMsg := newInMemoryMessage(ep.msg.SessionID, message.Tool, []message.ContentPart{
		message.ToolResult{
			ToolCallID: p.FunctionResponse.ID,
			Name:       p.FunctionResponse.Name,
			Content:    content,
			Data:       artifactPath,
			Metadata:   metadata,
			IsError:    isError,
		},
	}, "", "")
	toolMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	ep.broker.Publish(pubsub.CreatedEvent, toolMsg.Clone())

	// Check if the tool signaled that the turn should stop (dialog canceled).
	return ep.turnAbort != nil && ep.turnAbort.Load()
}

// extractFunctionResponseMetadata extracts the "metadata" key from a function response.
// Used by both the live event path and the replay path (eventsToMessages).
func extractFunctionResponseMetadata(resp *genai.FunctionResponse) string {
	if resp == nil || resp.Response == nil {
		return ""
	}
	v, ok := resp.Response["metadata"]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// extractFunctionResponseArtifactPath extracts the artifact_path from a station
// tool response, if present.
func extractFunctionResponseArtifactPath(resp *genai.FunctionResponse) string {
	if resp == nil || resp.Response == nil {
		return ""
	}
	v, ok := resp.Response["artifact_path"]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// bridgeTodos persists todos from tool metadata to the Crucible session.
// Uses the targeted UpdateTodos method to avoid clobbering other fields.
func (ep *eventProcessor) bridgeTodos(metadataJSON string) {
	if metadataJSON == "" {
		return
	}
	var meta tools.TodosResponseMetadata
	if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
		slog.Error("Failed to parse todos metadata", "error", err)
		return
	}
	if ep.sessions == nil {
		return
	}

	if err := ep.sessions.UpdateTodos(context.Background(), ep.msg.SessionID, meta.Todos); err != nil {
		slog.Error("Failed to update session todos", "error", err)
		return
	}

	// Mark per-session todo activity for staleness tracking.
	if ep.lastTodoUpdate != nil {
		ep.lastTodoUpdate.Set(ep.msg.SessionID, time.Now())
	}
}

func (ep *eventProcessor) handleFinish(event *adksession.Event, hasFunctionCall bool) {
	// Capture finish reason from non-partial events.
	// Skip for function-call events (intermediate tool-use turns) — the agent
	// continues after tool execution, so this isn't the real end of turn.
	if !hasFunctionCall && !event.Partial && event.FinishReason != "" && event.FinishReason != genai.FinishReasonUnspecified {
		slog.Debug("Event finish reason",
			"reason", string(event.FinishReason),
			"error_code", event.ErrorCode,
			"error_message", event.ErrorMessage,
			"partial", event.Partial,
		)
		ep.msg.FinishThinking()
		finishReason := mapFinishReason(event.FinishReason)
		var errMsg string
		if finishReason == message.FinishReasonError && event.ErrorMessage != "" {
			errMsg = event.ErrorMessage
		}
		ep.msg.AddFinish(finishReason, "", errMsg)
		ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())
	}

	// Handle model-level errors (independent of finish reason).
	if event.ErrorCode != "" {
		slog.Debug("Event error",
			"error_code", event.ErrorCode,
			"error_message", event.ErrorMessage,
		)
		ep.msg.AddFinish(message.FinishReasonError, event.ErrorCode, event.ErrorMessage)
		ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())
	}
}

// publishGrounding adds or updates GroundingContent on the message from GroundingMetadata
// and/or CitationMetadata. Supports progressive streaming: later events merge into existing.
func (ep *eventProcessor) publishGrounding(gm *genai.GroundingMetadata, cm *genai.CitationMetadata) {
	gc, ok := buildGroundingUpdate(gm, cm, ep.msg)
	if !ok {
		return
	}
	ep.msg.Parts = append(ep.msg.Parts, gc)
	ep.broker.Publish(pubsub.UpdatedEvent, ep.msg.Clone())
}

// buildGroundingUpdate merges grounding/citation metadata into the message's parts.
// If an existing GroundingContent is found, it is updated in-place in msg.Parts and
// ok=false is returned (no append needed). Otherwise the new GroundingContent is
// returned for the caller to append.
func buildGroundingUpdate(gm *genai.GroundingMetadata, cm *genai.CitationMetadata, msg *message.Message) (message.GroundingContent, bool) {
	hasGrounding := gm != nil && (len(gm.WebSearchQueries) > 0 || len(gm.GroundingChunks) > 0)
	hasCitations := cm != nil && len(cm.Citations) > 0
	if !hasGrounding && !hasCitations {
		return message.GroundingContent{}, false
	}

	// Find existing GroundingContent index for in-place update.
	for i, part := range msg.Parts {
		if existing, ok := part.(message.GroundingContent); ok {
			mergeGrounding(&existing, gm, cm, hasGrounding, hasCitations)
			msg.Parts[i] = existing // write back to slice
			return message.GroundingContent{}, false
		}
	}

	// First grounding event — build new GroundingContent.
	gc := newGroundingContent(gm, cm, hasCitations)
	return gc, true
}

// mergeGrounding progressively updates an existing GroundingContent with new data.
func mergeGrounding(existing *message.GroundingContent, gm *genai.GroundingMetadata, cm *genai.CitationMetadata, hasGrounding, hasCitations bool) {
	if hasGrounding {
		fresh := groundingFromMetadata(gm)
		if len(existing.Sources) == 0 {
			existing.Sources = fresh.Sources
		}
		if len(existing.Queries) == 0 {
			existing.Queries = fresh.Queries
		}
		if len(fresh.Supports) > 0 {
			existing.Supports = fresh.Supports
		}
	}
	if hasCitations {
		existing.Citations = citationsFromMetadata(cm)
	}
}

// newGroundingContent creates a GroundingContent from metadata.
func newGroundingContent(gm *genai.GroundingMetadata, cm *genai.CitationMetadata, hasCitations bool) message.GroundingContent {
	var gc message.GroundingContent
	if gm != nil {
		gc = groundingFromMetadata(gm)
	}
	if hasCitations {
		gc.Citations = citationsFromMetadata(cm)
	}
	return gc
}

// streamThrottle controls the rate at which streaming updates are published to the UI.
// Instead of publishing on every token, it batches updates so text appears in smooth bursts.
// No timers or goroutines — just a time check. The finish event always flushes immediately.
type streamThrottle struct {
	interval    time.Duration
	lastPublish time.Time
	broker      *pubsub.Broker[message.Message]
}

const streamBufferInterval = 120 * time.Millisecond

func newStreamThrottle(broker *pubsub.Broker[message.Message]) *streamThrottle {
	return &streamThrottle{
		interval: streamBufferInterval,
		broker:   broker,
	}
}

// publish publishes if enough time has passed since the last publish; otherwise skips.
func (st *streamThrottle) publish(msg *message.Message) {
	now := time.Now()
	if now.Sub(st.lastPublish) >= st.interval {
		st.lastPublish = now
		st.broker.Publish(pubsub.UpdatedEvent, msg.Clone())
	}
}

// flush publishes unconditionally (used for finish/error events).
func (st *streamThrottle) flush(msg *message.Message) {
	st.broker.Publish(pubsub.UpdatedEvent, msg.Clone())
	st.lastPublish = time.Now()
}

// eventHasFunctionResponse checks if an ADK event contains a FunctionResponse part,
// indicating a tool cycle just completed.
func eventHasFunctionResponse(event *adksession.Event) bool {
	if event == nil || event.Content == nil {
		return false
	}
	for _, p := range event.Content.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// publishAssistantPlaceholder creates an empty assistant message and publishes
// it to the broker so the UI shows the message immediately.
func publishAssistantPlaceholder(
	broker *pubsub.Broker[message.Message],
	sessionID, model, provider string,
) message.Message {
	msg := newInMemoryMessage(sessionID, message.Assistant, []message.ContentPart{}, model, provider)
	broker.Publish(pubsub.CreatedEvent, msg.Clone())
	return msg
}

// finalizeAssistantMessage ensures the message has a finish marker, attaches
// token usage, and flushes it to the broker.
func finalizeAssistantMessage(
	throttle *streamThrottle,
	msg *message.Message,
	usage UsageInfo,
) {
	if !msg.IsFinished() {
		msg.AddFinish(message.FinishReasonEndTurn, "", "")
	}
	msg.SetFinishTokens(usage.PromptTokens, usage.CandidatesTokens, usage.TotalTokens)
	throttle.flush(msg)
}

// groundingFromMetadata converts genai.GroundingMetadata to a message.GroundingContent.
func groundingFromMetadata(gm *genai.GroundingMetadata) message.GroundingContent {
	gc := message.GroundingContent{
		Queries: gm.WebSearchQueries,
	}
	for _, chunk := range gm.GroundingChunks {
		if chunk.Web != nil && chunk.Web.URI != "" {
			title := chunk.Web.Title
			if title == "" {
				title = chunk.Web.URI
			}
			gc.Sources = append(gc.Sources, message.GroundingSource{
				Title:  title,
				URL:    chunk.Web.URI,
				Domain: extractDomain(chunk.Web.URI),
			})
		}
	}
	for _, s := range gm.GroundingSupports {
		gs := message.GroundingSupport{
			ChunkIndices: s.GroundingChunkIndices,
			Scores:       s.ConfidenceScores,
		}
		if s.Segment != nil {
			gs.Text = s.Segment.Text
		}
		gc.Supports = append(gc.Supports, gs)
	}
	return gc
}

// extractDomain returns the hostname from a URL, stripping the port.
// Falls back to the raw URI if parsing fails.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL
	}
	return u.Hostname()
}

// citationsFromMetadata converts genai.CitationMetadata to message.GroundingCitation slice.
func citationsFromMetadata(cm *genai.CitationMetadata) []message.GroundingCitation {
	if cm == nil || len(cm.Citations) == 0 {
		return nil
	}
	out := make([]message.GroundingCitation, len(cm.Citations))
	for i, c := range cm.Citations {
		out[i] = message.GroundingCitation{
			StartIndex: c.StartIndex,
			EndIndex:   c.EndIndex,
			URI:        c.URI,
			Title:      c.Title,
			License:    c.License,
		}
	}
	return out
}
