package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock for decorator tests ---

// staleMockService simulates the ADK database session service's optimistic
// concurrency behavior. It tracks a "DB timestamp" and compares it against
// the session's LastUpdateTime to decide whether AppendEvent should fail.
type staleMockService struct {
	dbTimestamp   time.Time          // simulated DB update_time
	appendedCount int                // successful appends
	getSession    adksession.Session // what Get returns (nil = error)
	getFails      bool               // if true, Get returns an error
}

func (s *staleMockService) AppendEvent(_ context.Context, sess adksession.Session, _ *adksession.Event) error {
	if sess.LastUpdateTime().Before(s.dbTimestamp) {
		return fmt.Errorf(
			"stale session error: last update time from request (%s) is older than in database (%s)",
			sess.LastUpdateTime().Format(time.RFC3339Nano),
			s.dbTimestamp.Format(time.RFC3339Nano),
		)
	}
	// Simulate DB advancing the timestamp on successful append.
	s.dbTimestamp = s.dbTimestamp.Add(time.Millisecond)
	s.appendedCount++
	return nil
}

func (s *staleMockService) Get(_ context.Context, _ *adksession.GetRequest) (*adksession.GetResponse, error) {
	if s.getFails {
		return nil, errors.New("mock: Get failed")
	}
	if s.getSession == nil {
		return nil, errors.New("mock: no session configured")
	}
	return &adksession.GetResponse{Session: s.getSession}, nil
}

func (s *staleMockService) Create(_ context.Context, _ *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *staleMockService) List(_ context.Context, _ *adksession.ListRequest) (*adksession.ListResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *staleMockService) Delete(_ context.Context, _ *adksession.DeleteRequest) error {
	return errors.New("not implemented")
}

// timedMockSession implements adksession.Session with a configurable
// LastUpdateTime, simulating the real localSession's timestamp behavior.
type timedMockSession struct {
	id        string
	updatedAt time.Time
}

func (m *timedMockSession) ID() string                { return m.id }
func (m *timedMockSession) AppName() string           { return "crucible" }
func (m *timedMockSession) UserID() string            { return "user" }
func (m *timedMockSession) State() adksession.State   { return nil }
func (m *timedMockSession) Events() adksession.Events { return nil }
func (m *timedMockSession) LastUpdateTime() time.Time { return m.updatedAt }

// --- tests ---

func TestRetryService_NoStale(t *testing.T) {
	t.Parallel()

	now := time.Now()
	inner := &staleMockService{dbTimestamp: now}
	svc := NewRetrySessionService(inner)

	sess := &timedMockSession{id: "s1", updatedAt: now}
	event := adksession.NewEvent("inv-1")
	event.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "hello"}}}

	err := svc.AppendEvent(context.Background(), sess, event)

	require.NoError(t, err)
	assert.Equal(t, 1, inner.appendedCount)
}

func TestRetryService_StaleRetrySucceeds(t *testing.T) {
	t.Parallel()

	t0 := time.Now()
	t1 := t0.Add(time.Second) // DB has advanced past the session's timestamp

	// Fresh session returned by Get has the current DB timestamp.
	freshSess := &timedMockSession{id: "s1", updatedAt: t1}

	inner := &staleMockService{
		dbTimestamp: t1,
		getSession:  freshSess,
	}
	svc := NewRetrySessionService(inner)

	// Caller's session is stale (updatedAt = t0 < dbTimestamp = t1).
	staleSess := &timedMockSession{id: "s1", updatedAt: t0}
	event := adksession.NewEvent("inv-1")
	event.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "retried"}}}

	err := svc.AppendEvent(context.Background(), staleSess, event)

	require.NoError(t, err)
	assert.Equal(t, 1, inner.appendedCount, "should succeed on retry")
}

func TestRetryService_StaleGetFails(t *testing.T) {
	t.Parallel()

	t0 := time.Now()
	t1 := t0.Add(time.Second)

	inner := &staleMockService{
		dbTimestamp: t1,
		getFails:    true,
	}
	svc := NewRetrySessionService(inner)

	staleSess := &timedMockSession{id: "s1", updatedAt: t0}
	event := adksession.NewEvent("inv-1")
	event.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "fail"}}}

	err := svc.AppendEvent(context.Background(), staleSess, event)

	require.Error(t, err)
	// Both the original stale error and the refresh failure should be present.
	assert.Contains(t, err.Error(), "stale session error")
	assert.Contains(t, err.Error(), "refresh failed")
	assert.Equal(t, 0, inner.appendedCount)
}

func TestRetryService_NonStaleError(t *testing.T) {
	t.Parallel()

	// Inner service that always returns a non-stale error.
	inner := &alwaysFailService{err: errors.New("connection refused")}
	svc := NewRetrySessionService(inner)

	sess := &timedMockSession{id: "s1", updatedAt: time.Now()}
	event := adksession.NewEvent("inv-1")
	event.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "x"}}}

	err := svc.AppendEvent(context.Background(), sess, event)

	require.Error(t, err)
	assert.Equal(t, "connection refused", err.Error())
	assert.Equal(t, 1, inner.callCount, "should not retry on non-stale errors")
}

func TestRetryService_DelegatesOtherMethods(t *testing.T) {
	t.Parallel()

	inner := adksession.InMemoryService()
	svc := NewRetrySessionService(inner)

	// Create should delegate.
	resp, err := svc.Create(context.Background(), &adksession.CreateRequest{
		AppName: "test", UserID: "u1", SessionID: "s1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Session)

	// Get should delegate.
	getResp, err := svc.Get(context.Background(), &adksession.GetRequest{
		AppName: "test", UserID: "u1", SessionID: "s1",
	})
	require.NoError(t, err)
	assert.Equal(t, "s1", getResp.Session.ID())

	// List should delegate.
	listResp, err := svc.List(context.Background(), &adksession.ListRequest{
		AppName: "test", UserID: "u1",
	})
	require.NoError(t, err)
	assert.Len(t, listResp.Sessions, 1)

	// Delete should delegate.
	err = svc.Delete(context.Background(), &adksession.DeleteRequest{
		AppName: "test", UserID: "u1", SessionID: "s1",
	})
	require.NoError(t, err)
}

func TestRetryService_MultipleStaleCallsAllRetry(t *testing.T) {
	t.Parallel()

	// Simulate the real scenario: runner and midloop hold different sessions.
	// After one appends, the other's session is stale.
	t0 := time.Now()
	dbTime := t0

	freshSessForGet := &timedMockSession{id: "s1"}

	inner := &staleMockService{
		dbTimestamp: dbTime,
	}
	svc := NewRetrySessionService(inner)

	// First call from "runner" — session is current, succeeds directly.
	runnerSess := &timedMockSession{id: "s1", updatedAt: t0}
	event1 := adksession.NewEvent("inv-1")
	event1.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "e1"}}}
	err := svc.AppendEvent(context.Background(), runnerSess, event1)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.appendedCount)

	// DB has advanced. Now "midloop" tries with a stale session.
	// Set up Get to return a fresh session with the current DB timestamp.
	freshSessForGet.updatedAt = inner.dbTimestamp
	inner.getSession = freshSessForGet

	midloopSess := &timedMockSession{id: "s1", updatedAt: t0} // stale
	event2 := adksession.NewEvent("inv-1")
	event2.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "e2"}}}
	err = svc.AppendEvent(context.Background(), midloopSess, event2)
	require.NoError(t, err)
	assert.Equal(t, 2, inner.appendedCount)

	// Now runner tries again — its session is stale too (DB advanced by midloop).
	freshSessForGet.updatedAt = inner.dbTimestamp
	event3 := adksession.NewEvent("inv-1")
	event3.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "e3"}}}
	err = svc.AppendEvent(context.Background(), runnerSess, event3)
	require.NoError(t, err)
	assert.Equal(t, 3, inner.appendedCount)
}

func TestIsStaleSessionError(t *testing.T) {
	t.Parallel()

	assert.True(t, isStaleSessionError(errors.New("stale session error: timestamps differ")))
	assert.True(t, isStaleSessionError(fmt.Errorf("wrapped: %w", errors.New("stale session error: x"))))
	assert.False(t, isStaleSessionError(errors.New("connection refused")))
	assert.False(t, isStaleSessionError(nil))
}

// alwaysFailService is a mock that always fails AppendEvent with a given error.
type alwaysFailService struct {
	err       error
	callCount int
}

func (a *alwaysFailService) AppendEvent(_ context.Context, _ adksession.Session, _ *adksession.Event) error {
	a.callCount++
	return a.err
}

func (a *alwaysFailService) Create(_ context.Context, _ *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return nil, errors.New("not implemented")
}

func (a *alwaysFailService) Get(_ context.Context, _ *adksession.GetRequest) (*adksession.GetResponse, error) {
	return nil, errors.New("not implemented")
}

func (a *alwaysFailService) List(_ context.Context, _ *adksession.ListRequest) (*adksession.ListResponse, error) {
	return nil, errors.New("not implemented")
}

func (a *alwaysFailService) Delete(_ context.Context, _ *adksession.DeleteRequest) error {
	return errors.New("not implemented")
}
