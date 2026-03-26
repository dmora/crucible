package global

import (
	"context"
	"log/slog"
)

// Writer performs best-effort writes to the global session index.
// All errors are logged and swallowed — the global index is advisory.
type Writer struct {
	q        *Queries
	project  string
	model    string
	provider string
}

// NewWriter creates a Writer that tags all rows with the given project,
// model, and provider.
func NewWriter(q *Queries, project, model, provider string) *Writer {
	return &Writer{q: q, project: project, model: model, provider: provider}
}

// SetModelInfo updates the model and provider stored on new writes.
func (w *Writer) SetModelInfo(model, provider string) {
	w.model = model
	w.provider = provider
}

// Create upserts a new session row with zero tokens/cost.
func (w *Writer) Create(ctx context.Context, id, title string, createdAt int64) {
	if err := w.q.UpsertSession(ctx, UpsertSessionParams{
		ID: id, Project: w.project, Title: title,
		Tokens: 0, Cost: 0.0,
		PromptTokens: 0, CompletionTokens: 0, StationTokens: 0,
		Model: w.model, Provider: w.provider,
		Status: "active", WorktreeBranch: "",
		CreatedAt: createdAt,
	}); err != nil {
		slog.Warn("global index: create failed", "session_id", id, "error", err)
	}
}

// UpdateTitleAndUsage upserts title and adds prompt/completion/cost as deltas.
// Does NOT change total_tokens — matches local UpdateSessionTitleAndUsage.
func (w *Writer) UpdateTitleAndUsage(ctx context.Context, id, title string, promptTokensDelta, completionTokensDelta int64, costDelta float64) {
	if err := w.q.UpsertSessionTitleAndUsage(ctx, UpsertSessionTitleAndUsageParams{
		ID: id, Project: w.project, Title: title,
		PromptTokens: promptTokensDelta, CompletionTokens: completionTokensDelta,
		Cost: costDelta,
	}); err != nil {
		slog.Warn("global index: update title+usage failed", "session_id", id, "error", err)
	}
}

// UpdateUsage upserts absolute prompt/completion + additive total/station/cost.
func (w *Writer) UpdateUsage(ctx context.Context, id string, promptTokens, completionTokens, totalTokensDelta, stationTokensDelta int64, costDelta float64) {
	if err := w.q.UpsertSessionUsage(ctx, UpsertSessionUsageParams{
		ID: id, Project: w.project,
		Tokens: totalTokensDelta, Cost: costDelta,
		PromptTokens: promptTokens, CompletionTokens: completionTokens,
		StationTokens: stationTokensDelta,
	}); err != nil {
		slog.Warn("global index: update usage failed", "session_id", id, "error", err)
	}
}

// Save upserts with absolute values for all token columns.
func (w *Writer) Save(ctx context.Context, id, title string, tokens int64, cost float64, promptTokens, completionTokens, stationTokens int64, createdAt int64) {
	if err := w.q.UpsertSession(ctx, UpsertSessionParams{
		ID: id, Project: w.project, Title: title,
		Tokens: tokens, Cost: cost,
		PromptTokens: promptTokens, CompletionTokens: completionTokens, StationTokens: stationTokens,
		Model: w.model, Provider: w.provider,
		Status: "active", WorktreeBranch: "",
		CreatedAt: createdAt,
	}); err != nil {
		slog.Warn("global index: save failed", "session_id", id, "error", err)
	}
}

// SetWorktreeBranch updates the worktree branch without bumping updated_at.
func (w *Writer) SetWorktreeBranch(ctx context.Context, id, branch string) {
	if err := w.q.UpdateWorktreeBranch(ctx, UpdateWorktreeBranchParams{
		WorktreeBranch: branch, ID: id,
	}); err != nil {
		slog.Warn("global index: set worktree branch failed", "session_id", id, "error", err)
	}
}

// CompleteSession marks a session as completed.
func (w *Writer) CompleteSession(ctx context.Context, id string) {
	if err := w.q.CompleteSession(ctx, id); err != nil {
		slog.Warn("global index: complete session failed", "session_id", id, "error", err)
	}
}

// Delete removes a session from the global index.
func (w *Writer) Delete(ctx context.Context, id string) {
	if err := w.q.DeleteSession(ctx, id); err != nil {
		slog.Warn("global index: delete failed", "session_id", id, "error", err)
	}
}
