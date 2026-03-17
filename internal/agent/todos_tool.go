package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/session"
)

// todosInput is the schema for the todos function tool.
type todosInput struct {
	Todos []todosInputItem `json:"todos" description:"The complete todo list. Always send the full list, not incremental updates."`
}

// todosInputItem is a single item in the todos input.
type todosInputItem struct {
	Content    string `json:"content"               description:"What needs to be done"`
	Status     string `json:"status"                description:"One of: pending, in_progress, completed"`
	ActiveForm string `json:"active_form,omitempty"  description:"Present continuous verb for in-progress display (e.g. 'Drafting the API spec')"`
}

// todosOutput is the return schema for the todos function tool.
type todosOutput struct {
	Result   string `json:"result"             description:"Human-readable summary of the update"`
	Metadata string `json:"metadata,omitempty" description:"Internal rendering data"`
}

var validStatuses = map[string]session.TodoStatus{
	string(session.TodoStatusPending):    session.TodoStatusPending,
	string(session.TodoStatusInProgress): session.TodoStatusInProgress,
	string(session.TodoStatusCompleted):  session.TodoStatusCompleted,
}

// validateTodosInput validates the input items and converts them to session.Todo slice.
func validateTodosInput(items []todosInputItem) ([]session.Todo, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("todos list cannot be empty")
	}
	todos := make([]session.Todo, len(items))
	var inProgressCount int
	for i, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			return nil, fmt.Errorf("todo %d: content cannot be empty", i)
		}
		status, ok := validStatuses[item.Status]
		if !ok {
			return nil, fmt.Errorf("todo %d: invalid status %q (must be pending, in_progress, or completed)", i, item.Status)
		}
		activeForm := strings.TrimSpace(item.ActiveForm)
		if status == session.TodoStatusInProgress {
			inProgressCount++
			if inProgressCount > 1 {
				return nil, fmt.Errorf("todo %d: at most one item can be in_progress at a time", i)
			}
			if activeForm == "" {
				activeForm = content
			}
		}
		todos[i] = session.Todo{
			Content:    content,
			Status:     status,
			ActiveForm: activeForm,
		}
	}
	return todos, nil
}

// computeTodoDiff computes the diff between old and new todo lists,
// returning a TodosResponseMetadata with diff results and the new list.
func computeTodoDiff(oldTodos, newTodos []session.Todo) tools.TodosResponseMetadata {
	isNew := len(oldTodos) == 0

	// Build lookup from old todos: content → status.
	oldStatus := make(map[string]session.TodoStatus, len(oldTodos))
	for _, t := range oldTodos {
		oldStatus[t.Content] = t.Status
	}

	var justCompleted []string
	var justStarted string
	var completed, total int

	for _, t := range newTodos {
		total++
		if t.Status == session.TodoStatusCompleted {
			completed++
			// Was it not-completed before?
			if prev, exists := oldStatus[t.Content]; exists && prev != session.TodoStatusCompleted {
				justCompleted = append(justCompleted, t.Content)
			}
		}
		if t.Status == session.TodoStatusInProgress && justStarted == "" {
			if prev, exists := oldStatus[t.Content]; !exists || prev != session.TodoStatusInProgress {
				justStarted = t.Content
			}
		}
	}

	return tools.TodosResponseMetadata{
		IsNew:         isNew,
		Todos:         newTodos,
		JustCompleted: justCompleted,
		JustStarted:   justStarted,
		Completed:     completed,
		Total:         total,
	}
}

// newTodosTool creates an ADK function tool for supervisor work tracking.
func newTodosTool(sessions session.Service, sessionID string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: tools.TodosToolName,
		Description: "Track your work plan. Call this at the start of multi-step work to create a checklist, " +
			"then update it as you complete each step. Always send the COMPLETE list (not diffs). " +
			"Status values: pending, in_progress, completed. Set active_form for in-progress items.",
	}, func(_ tool.Context, input todosInput) (todosOutput, error) {
		newTodos, err := validateTodosInput(input.Todos)
		if err != nil {
			return todosOutput{}, err
		}

		// Read current session for diff (read-only).
		var oldTodos []session.Todo
		if sessions != nil {
			sess, getErr := sessions.Get(context.Background(), sessionID)
			if getErr == nil {
				oldTodos = sess.Todos
			}
		}

		meta := computeTodoDiff(oldTodos, newTodos)

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return todosOutput{}, fmt.Errorf("failed to marshal metadata: %w", err)
		}

		return todosOutput{
			Result:   fmt.Sprintf("Updated todos: %d/%d completed", meta.Completed, meta.Total),
			Metadata: string(metaJSON),
		}, nil
	})
}
