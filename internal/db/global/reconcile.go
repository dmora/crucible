package global

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/projects"
)

const (
	statusActive    = "active"
	statusCompleted = "completed"
)

// Reconcile performs a full sync of the global index from all registered
// project databases. It upserts all sessions found in local DBs and
// deletes stale rows that no longer exist in any project.
func Reconcile(ctx context.Context, globalDB *sql.DB) error {
	projectList, err := projects.List()
	if err != nil {
		return err
	}

	globalQ := New(globalDB)
	liveIDs := map[string]bool{}

	for _, proj := range projectList {
		reconcileProject(ctx, globalQ, proj, liveIDs)
	}

	// Mark sessions from missing projects as abandoned.
	markOrphanProjects(ctx, globalQ, projectList)

	staleCount, err := deleteStaleRows(ctx, globalQ, liveIDs)
	if err != nil {
		return err
	}

	slog.Info("Reconciled global index",
		"sessions", len(liveIDs),
		"projects", len(projectList),
		"stale_removed", staleCount,
	)
	return nil
}

// reconcileProject syncs all sessions from a single project into the global index.
func reconcileProject(ctx context.Context, globalQ *Queries, proj projects.Project, liveIDs map[string]bool) {
	localDB, err := db.Connect(ctx, proj.DataDir)
	if err != nil {
		slog.Warn("Reconcile: skipping project", "project", proj.Path, "error", err)
		return
	}
	defer localDB.Close()

	localQ := db.New(localDB)
	sessions, err := localQ.ListSessions(ctx)
	if err != nil {
		slog.Warn("Reconcile: failed to list sessions", "project", proj.Path, "error", err)
		return
	}

	for _, s := range sessions {
		liveIDs[s.ID] = true
		status := deriveStatus(s.Todos.String)

		if err := globalQ.UpsertSession(ctx, UpsertSessionParams{
			ID: s.ID, Project: proj.Path, Title: s.Title,
			Tokens: s.TotalTokens, Cost: s.Cost,
			PromptTokens: s.PromptTokens, CompletionTokens: s.CompletionTokens,
			StationTokens: s.StationTokens,
			Model:         "", Provider: "", // live-only — not stored locally
			Status:         status,
			WorktreeBranch: "",
			CreatedAt:      s.CreatedAt,
		}); err != nil {
			slog.Warn("Reconcile: upsert failed", "session_id", s.ID, "error", err)
		}
	}
}

// deriveStatus returns statusCompleted if all todos are done, otherwise statusActive.
func deriveStatus(todosJSON string) string {
	if todosJSON == "" {
		return statusActive
	}
	var todos []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(todosJSON), &todos); err != nil || len(todos) == 0 {
		return statusActive
	}
	for _, t := range todos {
		if t.Status != statusCompleted {
			return statusActive
		}
	}
	return statusCompleted
}

// markOrphanProjects marks sessions for projects whose directories no longer exist.
func markOrphanProjects(ctx context.Context, globalQ *Queries, projectList []projects.Project) {
	for _, proj := range projectList {
		if _, err := os.Stat(proj.Path); os.IsNotExist(err) {
			if err := globalQ.AbandonOrphanSessions(ctx, proj.Path); err != nil {
				slog.Warn("Reconcile: abandon orphans failed", "project", proj.Path, "error", err)
			}
		}
	}
}

// deleteStaleRows removes global index rows not present in any local DB.
func deleteStaleRows(ctx context.Context, globalQ *Queries, liveIDs map[string]bool) (int, error) {
	existingIDs, err := globalQ.ListSessionIDs(ctx)
	if err != nil {
		return 0, err
	}
	staleCount := 0
	for _, id := range existingIDs {
		if !liveIDs[id] {
			if err := globalQ.DeleteSession(ctx, id); err != nil {
				slog.Warn("Reconcile: delete stale failed", "session_id", id, "error", err)
			} else {
				staleCount++
			}
		}
	}
	return staleCount, nil
}
