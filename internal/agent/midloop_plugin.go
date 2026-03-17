package agent

import (
	"fmt"
	"log/slog"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/csync"
)

// midLoopPlugin drains queued user messages between model calls and injects
// them into the current LLM request. Messages are persisted as first-class
// user events via AppendEvent before injection, so they survive restarts.
//
// Ordering: midloop → steering → logging (midloop must run first so steering
// sees the updated conversation state).
type midLoopPlugin struct {
	queue             *csync.Map[string, []SessionAgentCall]
	activeADKSessions *csync.Map[string, adksession.Session]
	sessionService    adksession.Service
}

// beforeModel is called by ADK before each GenerateContent call.
// It drains all queued messages for the current session, persists them as
// user events, and injects XML-wrapped parts into req.Contents.
func (p *midLoopPlugin) beforeModel(ctx adkagent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	// Don't do work if the context is already canceled — let the cancel
	// handler clean up. Return nil,nil so ADK doesn't treat this as a
	// plugin error.
	if ctx.Err() != nil {
		return nil, nil //nolint:nilerr // intentional: cancel handler owns cleanup
	}

	sessionID := ctx.SessionID()
	queued, ok := p.queue.Take(sessionID)
	if !ok || len(queued) == 0 {
		return nil, nil
	}

	adkSess, ok := p.activeADKSessions.Get(sessionID)
	if !ok {
		// No active session — re-queue everything and return error so runner
		// can surface it. This shouldn't happen in practice.
		// Use PrependSlice to avoid overwriting messages that arrived after Take().
		csync.PrependSlice(p.queue, sessionID, queued)
		return nil, fmt.Errorf("midloop: no active ADK session for %q", sessionID)
	}

	for i, call := range queued {
		rawParts := callToGenaiParts(call)

		// Persist raw parts as a first-class user event (no XML wrapping).
		// This ensures the message appears in the correct position on reload.
		event := adksession.NewEvent(ctx.InvocationID())
		event.Author = "user"
		event.Branch = ctx.Branch()
		event.Content = &genai.Content{Role: genai.RoleUser, Parts: rawParts}

		if err := p.sessionService.AppendEvent(ctx, adkSess, event); err != nil {
			// Re-queue the remaining messages (including the failed one).
			// Use PrependSlice to avoid overwriting messages that arrived after Take().
			csync.PrependSlice(p.queue, sessionID, queued[i:])
			return nil, fmt.Errorf("midloop: AppendEvent failed: %w", err)
		}

		// Wrap text parts in <operator_message> XML for model disambiguation.
		// Binary/inline-data parts pass through unwrapped.
		xmlParts := wrapOperatorMessage(rawParts, i+1)

		// Inject into req.Contents respecting Gemini's alternation rules.
		injectUserParts(req, xmlParts)
	}

	slog.Debug("Mid-loop injection",
		"session_id", sessionID,
		"count", len(queued),
	)
	return nil, nil
}

// wrapOperatorMessage wraps text parts in <operator_message seq="N"> XML.
// Non-text parts (InlineData) pass through unchanged.
func wrapOperatorMessage(parts []*genai.Part, seq int) []*genai.Part {
	wrapped := make([]*genai.Part, 0, len(parts))
	for _, p := range parts {
		if p.Text != "" {
			wrapped = append(wrapped, &genai.Part{
				Text: fmt.Sprintf("<user_message seq=\"%d\">\n%s\n</user_message>", seq, p.Text),
			})
		} else {
			wrapped = append(wrapped, p)
		}
	}
	return wrapped
}

// injectUserParts appends user parts to req.Contents following Gemini's
// alternation rules:
//   - If the last content is user-role with only text/inline-data parts (no
//     FunctionResponse/FunctionCall), append to it.
//   - Otherwise, create a new user-role content.
func injectUserParts(req *adkmodel.LLMRequest, parts []*genai.Part) {
	if len(req.Contents) > 0 {
		last := req.Contents[len(req.Contents)-1]
		if last.Role == genai.RoleUser && !contentHasFunctionParts(last) {
			last.Parts = append(last.Parts, parts...)
			return
		}
	}
	req.Contents = append(req.Contents, &genai.Content{
		Role:  genai.RoleUser,
		Parts: parts,
	})
}

// contentHasFunctionParts returns true if the content contains FunctionCall or
// FunctionResponse parts (which should not be mixed with injected user text).
func contentHasFunctionParts(c *genai.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionCall != nil || p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// newMidLoopPlugin creates an ADK plugin that injects queued user messages
// into the running LLM loop via BeforeModel.
func newMidLoopPlugin(
	queue *csync.Map[string, []SessionAgentCall],
	activeADKSessions *csync.Map[string, adksession.Session],
	sessionService adksession.Service,
) *plugin.Plugin {
	p := &midLoopPlugin{
		queue:             queue,
		activeADKSessions: activeADKSessions,
		sessionService:    sessionService,
	}
	plug, _ := plugin.New(plugin.Config{
		Name:                "crucible_midloop",
		BeforeModelCallback: p.beforeModel,
	})
	return plug
}
