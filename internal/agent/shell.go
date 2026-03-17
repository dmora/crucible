package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/shell"
)

// ExecuteUserShell runs a shell command on behalf of the operator, publishes
// tool-call/result messages for UI rendering, and persists a <user_shell>
// event to the ADK session so the supervisor has context.
func (a *sessionAgent) ExecuteUserShell(ctx context.Context, sessionID, command string) error {
	if sessionID == "" {
		return ErrSessionMissing
	}

	// Track with cancellable context (uses "-shell" suffix like "-summarize").
	shellCtx, cancel := context.WithCancel(ctx)
	shellKey := sessionID + "-shell"
	handle := NewTaskHandle(cancel)
	// Atomically replace any previous shell request, cancelling it if present.
	if prev, ok := a.activeRequests.Swap(shellKey, handle); ok && prev != nil {
		prev.Cancel()
	}
	defer cancel()
	defer func() {
		// Only remove our entry — compare by handle ID so a replacement
		// request's entry is never accidentally deleted.
		a.activeRequests.DeleteFunc(shellKey, func(h *TaskHandle) bool {
			return h != nil && h.ID == handle.ID
		})
	}()

	// Build + publish assistant message with pending ToolCall.
	toolCallID := "shell-" + uuid.New().String()
	input := marshalBashParams(command)

	assistantMsg := newInMemoryMessage(sessionID, message.Assistant, []message.ContentPart{
		message.ToolCall{
			ID:    toolCallID,
			Name:  tools.BashToolName,
			Input: input,
			State: message.ToolStatePending,
		},
	}, "", "")
	assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	a.messageBroker.Publish(pubsub.CreatedEvent, assistantMsg.Clone())

	// Execute command — use worktree CWD if active, otherwise the base working dir.
	cwd := a.workingDir
	if wt, ok := a.worktreeInfos.Get(sessionID); ok {
		cwd = wt.ResolvedCWD
	}
	sh := shell.NewShell(&shell.Options{WorkingDir: cwd})
	stdout, stderr, execErr := sh.Exec(shellCtx, command)

	// Build output + exit code.
	output := stdout
	exitCode := 0
	isError := false
	if execErr != nil {
		isError = true
		exitCode = shell.ExitCode(execErr)
		if stderr != "" {
			if output != "" {
				output = output + "\n" + stderr
			} else {
				output = stderr
			}
		} else if output == "" {
			output = execErr.Error()
		}
	}
	if output == "" {
		output = tools.BashNoOutput
	}

	// Mark tool call done + publish result.
	assistantMsg.FinishToolCall(toolCallID)
	a.messageBroker.Publish(pubsub.UpdatedEvent, assistantMsg.Clone())

	toolResultMsg := newInMemoryMessage(sessionID, message.Tool, []message.ContentPart{
		message.ToolResult{
			ToolCallID: toolCallID,
			Name:       tools.BashToolName,
			Content:    output,
			IsError:    isError,
		},
	}, "", "")
	toolResultMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	a.messageBroker.Publish(pubsub.CreatedEvent, toolResultMsg.Clone())

	// Persist to ADK session as user event with <user_shell> XML.
	a.persistShellEvent(shellCtx, sessionID, command, output, exitCode)

	return nil
}

// persistShellEvent persists a <user_shell> event to the ADK session so the
// supervisor sees the command and output on subsequent turns.
func (a *sessionAgent) persistShellEvent(ctx context.Context, sessionID, command, output string, exitCode int) {
	adkSess, err := a.ensureADKSession(ctx, sessionID)
	if err != nil {
		slog.Error("Failed to get ADK session for shell event", "err", err, "session_id", sessionID)
		return
	}
	// Escape closing tags in output to prevent XML injection.
	safeOutput := strings.ReplaceAll(output, "</user_shell>", "&lt;/user_shell&gt;")
	xmlText := fmt.Sprintf("<user_shell command=%q exit_code=\"%d\">\n%s\n</user_shell>",
		command, exitCode, safeOutput)
	event := adksession.NewEvent("")
	event.Author = "user"
	event.Content = &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: xmlText}},
	}
	if err := a.adkSessionService.AppendEvent(ctx, adkSess, event); err != nil {
		slog.Error("Failed to persist shell event", "err", err, "session_id", sessionID)
	}
}

// marshalBashParams serializes a bash command into the standard BashParams JSON format.
func marshalBashParams(command string) string {
	b, _ := json.Marshal(tools.BashParams{Command: command, Description: "Operator shell command"})
	return string(b)
}
