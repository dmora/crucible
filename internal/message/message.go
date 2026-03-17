package message

import (
	"context"

	"github.com/dmora/crucible/internal/pubsub"
)

// Service provides read-only access to messages and pubsub subscription.
// Write operations are handled by the agent layer via pubsub.Broker.Publish
// and ADK's session service handles persistence automatically.
type Service interface {
	pubsub.Subscriber[Message]
	List(ctx context.Context, sessionID string) ([]Message, error)
	ListUserMessages(ctx context.Context, sessionID string) ([]Message, error)
	ListAllUserMessages(ctx context.Context) ([]Message, error)
}
