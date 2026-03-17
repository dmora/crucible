// Package askuser provides a blocking request/response service for structured
// questions from the supervisor LLM to the operator. It follows the proven
// permission.Service pattern: blocking request/response with pubsub bridge
// and pending channels.
package askuser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/google/uuid"
)

// ErrNonInteractive is returned when a question is asked in non-interactive mode.
var ErrNonInteractive = errors.New("ask_user is not available in non-interactive mode")

// ErrDuplicateQuestionID is returned when questions have duplicate IDs.
var ErrDuplicateQuestionID = errors.New("duplicate question IDs")

// ErrCanceled is the deterministic error returned when the operator cancels the dialog.
// The top-level "error" string is preserved on reload, so this must be a stable,
// recognizable string for the chat card to detect cancellation deterministically.
const ErrCanceledMessage = "User canceled the operation"

// Question represents a structured question for the operator.
type Question struct {
	ID          string   `json:"id"`           // stable identifier (e.g. "approach", "scope")
	Question    string   `json:"question"`     // full question text
	Header      string   `json:"header"`       // short chip label (max 12 chars)
	Options     []Option `json:"options"`      // 2-4 options; empty = free text
	MultiSelect bool     `json:"multi_select"` // allow multiple selections
}

// Option represents a selectable option for a question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Request represents a pending ask_user request.
type Request struct {
	ID         string
	SessionID  string
	ToolCallID string // ADK function call ID for matching chat card
	Questions  []Question
}

// Answer represents the operator's answer to a single question.
type Answer struct {
	ID       string   `json:"id"`       // echoes Question.ID
	Question string   `json:"question"` // echoes Question.Question (human-readable)
	Values   []string `json:"values"`   // selected labels; len>1 only for multi-select
}

// Response represents the operator's complete response to a request.
type Response struct {
	Answers  []Answer `json:"answers"`
	Canceled bool     `json:"canceled"`
}

// Service provides blocking request/response for structured operator questions.
type Service interface {
	pubsub.Subscriber[Request]
	Request(ctx context.Context, sessionID, toolCallID string, questions []Question) (*Response, error)
	Respond(requestID string, response Response)
	SetNonInteractive(skip bool)
	NonInteractive() bool
}

type service struct {
	*pubsub.Broker[Request]

	pendingRequests *csync.Map[string, chan Response]
	nonInteractive  atomic.Bool

	// requestMu serializes requests so only one dialog is shown at a time.
	// Intentionally global — Crucible's TUI is single-session: ui.go holds one
	// *session.Session, agent activeRequests uses SetNX per session, and the
	// dialog overlay is a single stack. A per-session mutex would add complexity
	// for a scenario that cannot occur.
	requestMu sync.Mutex
}

// NewService creates a new ask_user service.
func NewService(nonInteractive bool) Service {
	svc := &service{
		Broker:          pubsub.NewBroker[Request](),
		pendingRequests: csync.NewMap[string, chan Response](),
	}
	svc.nonInteractive.Store(nonInteractive)
	return svc
}

// Request blocks until the operator responds or the context is canceled.
func (s *service) Request(ctx context.Context, sessionID, toolCallID string, questions []Question) (*Response, error) {
	if s.nonInteractive.Load() {
		return nil, ErrNonInteractive
	}

	// Validate question ID uniqueness.
	seen := make(map[string]bool, len(questions))
	for _, q := range questions {
		if q.ID == "" {
			return nil, fmt.Errorf("%w: empty question ID", ErrDuplicateQuestionID)
		}
		if seen[q.ID] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateQuestionID, q.ID)
		}
		seen[q.ID] = true
	}

	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	req := Request{
		ID:         uuid.New().String(),
		SessionID:  sessionID,
		ToolCallID: toolCallID,
		Questions:  questions,
	}

	respCh := make(chan Response, 1)
	s.pendingRequests.Set(req.ID, respCh)
	defer s.pendingRequests.Del(req.ID)

	// Publish the request so the UI can open the dialog.
	s.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return &resp, nil
	}
}

// Respond delivers the operator's response to a pending request.
// No-op if the request ID is unknown (e.g., already timed out).
func (s *service) Respond(requestID string, response Response) {
	respCh, ok := s.pendingRequests.Get(requestID)
	if !ok {
		return
	}
	respCh <- response
}

// SetNonInteractive enables or disables non-interactive mode.
func (s *service) SetNonInteractive(skip bool) {
	s.nonInteractive.Store(skip)
}

// NonInteractive returns whether the service is in non-interactive mode.
func (s *service) NonInteractive() bool {
	return s.nonInteractive.Load()
}
