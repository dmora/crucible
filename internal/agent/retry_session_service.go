package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	adksession "google.golang.org/adk/session"
)

// retrySessionService wraps a session.Service to automatically retry
// AppendEvent on stale session errors. The ADK database service uses
// optimistic concurrency: it checks that the session's updatedAt matches the
// DB before writing. When two callers (e.g. runner + midloop plugin) hold
// different session objects, one will inevitably be stale after the other
// appends. This wrapper catches that error, re-fetches the session, and
// retries the append once.
type retrySessionService struct {
	inner adksession.Service
}

// NewRetrySessionService wraps an ADK session.Service with automatic stale
// session retry on AppendEvent.
func NewRetrySessionService(inner adksession.Service) adksession.Service {
	return &retrySessionService{inner: inner}
}

func (r *retrySessionService) AppendEvent(ctx context.Context, sess adksession.Session, event *adksession.Event) error {
	err := r.inner.AppendEvent(ctx, sess, event)
	if err == nil || !isStaleSessionError(err) {
		return err
	}

	slog.Debug("Stale session detected, re-fetching for retry",
		"session_id", sess.ID(),
	)

	resp, getErr := r.inner.Get(ctx, &adksession.GetRequest{
		AppName:   sess.AppName(),
		UserID:    sess.UserID(),
		SessionID: sess.ID(),
	})
	if getErr != nil {
		return fmt.Errorf("%w (refresh failed: %w)", err, getErr)
	}

	return r.inner.AppendEvent(ctx, resp.Session, event)
}

func (r *retrySessionService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return r.inner.Create(ctx, req)
}

func (r *retrySessionService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	return r.inner.Get(ctx, req)
}

func (r *retrySessionService) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	return r.inner.List(ctx, req)
}

func (r *retrySessionService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	return r.inner.Delete(ctx, req)
}

// isStaleSessionError checks if the error is a stale session error from ADK's
// database session service optimistic concurrency check.
func isStaleSessionError(err error) bool {
	if err == nil {
		return false
	}
	// Walk the error chain — the stale error may be wrapped.
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), "stale session error") {
			return true
		}
	}
	return strings.Contains(err.Error(), "stale session error")
}
