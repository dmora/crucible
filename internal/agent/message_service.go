package agent

import (
	"context"
	"fmt"

	adksession "google.golang.org/adk/session"

	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
)

// adkMessageService implements message.Service backed by ADK's session events.
// Read operations load from ADK events and convert to message.Message.
// The agent publishes directly to the embedded broker during streaming;
// ADK handles persistence via AppendEvent.
type adkMessageService struct {
	*pubsub.Broker[message.Message]
	sessionService adksession.Service
}

// NewADKMessageService creates a message.Service that reads from ADK session events
// and uses the provided broker for pubsub. The broker should also be passed to the
// agent layer for streaming publishes.
func NewADKMessageService(sessionService adksession.Service, broker *pubsub.Broker[message.Message]) message.Service {
	return &adkMessageService{
		Broker:         broker,
		sessionService: sessionService,
	}
}

func (s *adkMessageService) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	resp, err := s.sessionService.Get(ctx, &adksession.GetRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err != nil {
		if isSessionNotFoundError(err) {
			return nil, nil // Session doesn't exist yet — return empty list.
		}
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return eventsToMessages(resp.Session.Events(), sessionID), nil
}

func (s *adkMessageService) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	resp, err := s.sessionService.Get(ctx, &adksession.GetRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: sessionID,
	})
	if err != nil {
		if isSessionNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list user messages: %w", err)
	}
	return eventsToUserPrompts(resp.Session.Events(), sessionID), nil
}

func (s *adkMessageService) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	listResp, err := s.sessionService.List(ctx, &adksession.ListRequest{
		AppName: adkAppName,
		UserID:  adkUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("list ADK sessions: %w", err)
	}

	var allMessages []message.Message
	for _, sess := range listResp.Sessions {
		msgs := eventsToUserPrompts(sess.Events(), sess.ID())
		allMessages = append(allMessages, msgs...)
	}
	return allMessages, nil
}
