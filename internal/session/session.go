package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/db/global"
	"github.com/dmora/crucible/internal/event"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/google/uuid"
)

type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

type Todo struct {
	Content    string     `json:"content"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"active_form"`
}

// HasIncompleteTodos returns true if there are any non-completed todos.
func HasIncompleteTodos(todos []Todo) bool {
	for _, todo := range todos {
		if todo.Status != TodoStatusCompleted {
			return true
		}
	}
	return false
}

type Session struct {
	ID               string
	Title            string
	PromptTokens     int64   // supervisor fuel gauge (absolute)
	CompletionTokens int64   // supervisor fuel gauge (absolute)
	TotalTokens      int64   // factory-wide cumulative tokens
	StationTokens    int64   // station-only cumulative tokens
	Cost             float64 // factory-wide cumulative cost
	Todos            []Todo
	CreatedAt        int64
	UpdatedAt        int64
}

type Service interface {
	pubsub.Subscriber[Session]
	Create(ctx context.Context, title string) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	List(ctx context.Context) ([]Session, error)
	Save(ctx context.Context, session Session) (Session, error)
	UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens int64, cost float64) error
	UpdateTodos(ctx context.Context, sessionID string, todos []Todo) error
	UpdateUsage(ctx context.Context, sessionID string, promptTokens, completionTokens int64, totalTokensDelta, stationTokensDelta int64, costDelta float64) error
	Delete(ctx context.Context, id string) error
	ListIDs(ctx context.Context) ([]string, error)
}

type service struct {
	*pubsub.Broker[Session]
	db          *sql.DB
	q           *db.Queries
	preDelete   func(ctx context.Context, id string) // before DB delete (cancel agents, purge state)
	onDelete    func(ctx context.Context, id string) // after DB commit (clean up external resources)
	globalIndex *global.Writer                       // nil if not configured
}

func (s *service) Create(ctx context.Context, title string) (Session, error) {
	dbSession, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:    uuid.New().String(),
		Title: title,
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	if s.globalIndex != nil {
		s.globalIndex.Create(ctx, session.ID, session.Title, session.CreatedAt)
	}
	s.Publish(pubsub.CreatedEvent, session)
	event.SessionCreated()
	return session, nil
}

func (s *service) Delete(ctx context.Context, id string) error {
	// Pre-delete: cancel agents, stop processes, purge in-memory state
	// BEFORE the DB transaction so there are no races with a deleted row.
	if s.preDelete != nil {
		s.preDelete(ctx, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := s.q.WithTx(tx)

	dbSession, err := qtx.GetSessionByID(ctx, id)
	if err != nil {
		return err
	}
	if err = qtx.DeleteSessionMessages(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session messages: %w", err)
	}
	if err = qtx.DeleteSessionFiles(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session files: %w", err)
	}
	if err = qtx.DeleteSession(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	// Post-delete: clean up external resources (e.g., ADK session) after DB commit.
	if s.onDelete != nil {
		s.onDelete(ctx, id)
	}
	if s.globalIndex != nil {
		s.globalIndex.Delete(ctx, id)
	}

	session := s.fromDBItem(dbSession)
	s.Publish(pubsub.DeletedEvent, session)
	event.SessionDeleted()
	return nil
}

func (s *service) Get(ctx context.Context, id string) (Session, error) {
	dbSession, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, err
	}
	return s.fromDBItem(dbSession), nil
}

func (s *service) Save(ctx context.Context, session Session) (Session, error) {
	todosJSON, err := marshalTodos(session.Todos)
	if err != nil {
		return Session{}, err
	}

	dbSession, err := s.q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:               session.ID,
		Title:            session.Title,
		PromptTokens:     session.PromptTokens,
		CompletionTokens: session.CompletionTokens,
		TotalTokens:      session.TotalTokens,
		StationTokens:    session.StationTokens,
		Cost:             session.Cost,
		Todos: sql.NullString{
			String: todosJSON,
			Valid:  todosJSON != "",
		},
	})
	if err != nil {
		return Session{}, err
	}
	session = s.fromDBItem(dbSession)
	if s.globalIndex != nil {
		s.globalIndex.Save(ctx, session.ID, session.Title, session.TotalTokens, session.Cost, session.PromptTokens, session.CompletionTokens, session.StationTokens, session.CreatedAt)
	}
	s.Publish(pubsub.UpdatedEvent, session)
	return session, nil
}

// UpdateTitleAndUsage updates only the title and usage fields atomically.
// This is safer than fetching, modifying, and saving the entire session.
func (s *service) UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens int64, cost float64) error {
	if err := s.q.UpdateSessionTitleAndUsage(ctx, db.UpdateSessionTitleAndUsageParams{
		ID:               sessionID,
		Title:            title,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Cost:             cost,
	}); err != nil {
		return err
	}
	if s.globalIndex != nil {
		s.globalIndex.UpdateTitleAndUsage(ctx, sessionID, title, promptTokens, completionTokens, cost)
	}
	// Re-fetch and publish so the UI picks up the updated title.
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	s.Publish(pubsub.UpdatedEvent, session)
	return nil
}

// UpdateTodos atomically sets only the todos column for a session.
func (s *service) UpdateTodos(ctx context.Context, sessionID string, todos []Todo) error {
	todosJSON, err := marshalTodos(todos)
	if err != nil {
		return err
	}
	if err := s.q.UpdateSessionTodos(ctx, db.UpdateSessionTodosParams{
		ID:    sessionID,
		Todos: sql.NullString{String: todosJSON, Valid: todosJSON != ""},
	}); err != nil {
		return err
	}
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	s.Publish(pubsub.UpdatedEvent, session)
	return nil
}

// UpdateUsage atomically sets prompt/completion tokens (overwrite) and
// accumulates total_tokens, station_tokens, and cost (additive) without
// touching other columns.
func (s *service) UpdateUsage(ctx context.Context, sessionID string, promptTokens, completionTokens int64, totalTokensDelta, stationTokensDelta int64, costDelta float64) error {
	if err := s.q.UpdateSessionUsage(ctx, db.UpdateSessionUsageParams{
		ID:               sessionID,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokensDelta,
		StationTokens:    stationTokensDelta,
		Cost:             costDelta,
	}); err != nil {
		return err
	}
	if s.globalIndex != nil {
		s.globalIndex.UpdateUsage(ctx, sessionID, promptTokens, completionTokens, totalTokensDelta, stationTokensDelta, costDelta)
	}
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	s.Publish(pubsub.UpdatedEvent, session)
	return nil
}

func (s *service) List(ctx context.Context) ([]Session, error) {
	dbSessions, err := s.q.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, len(dbSessions))
	for i, dbSession := range dbSessions {
		sessions[i] = s.fromDBItem(dbSession)
	}
	return sessions, nil
}

func (s *service) ListIDs(ctx context.Context) ([]string, error) {
	sessions, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(sessions))
	for i, sess := range sessions {
		ids[i] = sess.ID
	}
	return ids, nil
}

func (s service) fromDBItem(item db.Session) Session {
	todos, err := unmarshalTodos(item.Todos.String)
	if err != nil {
		slog.Error("Failed to unmarshal todos", "session_id", item.ID, "error", err)
	}
	return Session{
		ID:               item.ID,
		Title:            item.Title,
		PromptTokens:     item.PromptTokens,
		CompletionTokens: item.CompletionTokens,
		TotalTokens:      item.TotalTokens,
		StationTokens:    item.StationTokens,
		Cost:             item.Cost,
		Todos:            todos,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

func marshalTodos(todos []Todo) (string, error) {
	if len(todos) == 0 {
		return "", nil
	}
	data, err := json.Marshal(todos)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalTodos(data string) ([]Todo, error) {
	if data == "" {
		return []Todo{}, nil
	}
	var todos []Todo
	if err := json.Unmarshal([]byte(data), &todos); err != nil {
		return []Todo{}, err
	}
	return todos, nil
}

// ServiceOption configures optional behavior on the session service.
type ServiceOption func(*service)

// WithPreDelete registers a callback invoked before the DB delete transaction.
// Use this to cancel agents, stop processes, and purge in-memory state so
// teardown completes before the session row is removed.
func WithPreDelete(fn func(ctx context.Context, id string)) ServiceOption {
	return func(s *service) { s.preDelete = fn }
}

// WithOnDelete registers a callback invoked after a session is deleted from the DB.
// Use this to clean up external resources (e.g., ADK session data).
func WithOnDelete(fn func(ctx context.Context, id string)) ServiceOption {
	return func(s *service) { s.onDelete = fn }
}

// WithGlobalIndex enables dual-write to the global session index.
func WithGlobalIndex(w *global.Writer) ServiceOption {
	return func(s *service) { s.globalIndex = w }
}

func NewService(q *db.Queries, conn *sql.DB, opts ...ServiceOption) Service {
	broker := pubsub.NewBroker[Session]()
	svc := &service{
		Broker: broker,
		db:     conn,
		q:      q,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// CreateAgentToolSessionID creates a session ID for agent tool sessions
// using the format "messageID$$toolCallID".
func CreateAgentToolSessionID(messageID, toolCallID string) string {
	return fmt.Sprintf("%s$$%s", messageID, toolCallID)
}

// ParseAgentToolSessionID parses an agent tool session ID into its components.
func ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool) {
	parts := strings.Split(sessionID, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// IsAgentToolSession checks if a session ID follows the agent tool session format.
func IsAgentToolSession(sessionID string) bool {
	_, _, ok := ParseAgentToolSessionID(sessionID)
	return ok
}
