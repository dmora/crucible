package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dmora/adk-go-extras/plugin/notify"
	"github.com/dmora/agentrun"
	"google.golang.org/adk/tool"
)

// ProcessOps provides the process lifecycle operations that RecoveryController
// needs from processManager. This interface decouples recovery from the
// full processManager.
type ProcessOps interface {
	GetOrStart(ctx context.Context, sessionID, prompt, resumeID string) (agentrun.Process, bool, error)
	KillProcess(ctx context.Context, sessionID string)
	RemoveFromPool(sessionID string)
	DefaultContextWindowSize() int
	ClearActivity(sessionID string)
}

// RecoveryController manages context exhaustion detection and transparent
// station replacement. Owns the ContextBuffer lifecycle.
type RecoveryController struct {
	station            string
	cwd                string
	cwdResolver        func(sessionID string) string // per-session CWD; nil = use rc.cwd
	model              string                        // for cost estimation fallback
	contextWindow      int
	captureRepoStateFn func(ctx context.Context, cwd string) (string, error)
}

// Run executes the station with context exhaustion recovery.
// handler receives the raw message stream for UI observation.
// Returns (resultIsError, error).
func (rc *RecoveryController) Run(
	ctx context.Context,
	tctx tool.Context,
	ops ProcessOps,
	sessionID string,
	task string,
	taskBuilder *TaskBuilder,
	result *strings.Builder,
	handler func(agentrun.Message) error,
	notifier *notify.Notifier,
	ledger *UsageLedger,
) (resultIsError bool, err error) {
	buf := NewContextBuffer(task)

	var resumeID string
	if v, err := tctx.State().Get(stationResumeStateKey(rc.station)); err == nil {
		if s, ok := v.(string); ok {
			resumeID = s
		}
	}

	for range maxGenerations + 1 {
		// Streaming gen > 0: empty initialPrompt — CLI starts waiting,
		// full prompt delivered via Send() in RunTurn.
		// Spawn-per-turn gen > 0: prompt baked into Start() CLI args.
		var initialPrompt string
		if buf.Generation() == 0 || taskBuilder.IsSpawnPerTurn() {
			initialPrompt = taskBuilder.Build(task, true)
		}
		proc, firstTurn, err := ops.GetOrStart(ctx, sessionID, initialPrompt, resumeID)
		if err != nil {
			return false, err
		}

		if gen := buf.Generation(); gen > 0 {
			if info, ok := processStates.Get(processStateKey(sessionID, rc.station)); ok {
				info.Generation = gen
				updateProcessState(processStateKey(sessionID, rc.station), info)
			}
		}

		var turnTask string
		if buf.Generation() == 0 {
			turnTask = taskBuilder.Build(task, firstTurn)
		} else if firstTurn {
			turnTask = taskBuilder.Build(task, true)
		}

		buf.StartTurn()
		ops.ClearActivity(sessionID)

		var lastStopReason agentrun.StopReason
		var lastContextUsed, lastContextSize int
		turnStart := time.Now()

		wrappedHandler := rc.buildWrappedHandler(
			buf, result, handler,
			&lastStopReason, &lastContextUsed, &lastContextSize,
			&resultIsError, ledger,
		)

		var turnErr error
		_, spawnPerTurn := proc.(agentrun.SequentialSender)
		if firstTurn && spawnPerTurn {
			slog.Debug("S1 turn path: drain-only (spawn-per-turn first turn)",
				"station", rc.station, "generation", buf.Generation())
			turnErr = drainFirstTurn(ctx, proc, wrappedHandler)
		} else {
			slog.Debug("S1 turn path: RunTurn",
				"station", rc.station, "generation", buf.Generation(),
				"first_turn", firstTurn)
			turnErr = agentrun.RunTurn(ctx, proc, turnTask, wrappedHandler)
		}
		buf.FinalizeTurn(time.Since(turnStart))

		if _, spawnPerTurn := proc.(agentrun.SequentialSender); spawnPerTurn {
			// Spawn-per-turn processes exit after one turn — just remove from
			// pool without killing UI state (KillProcess calls removeProcessState).
			ops.RemoveFromPool(sessionID)
			return resultIsError, turnErr
		}

		if lastContextUsed == 0 {
			if info, ok := processStates.Get(processStateKey(sessionID, rc.station)); ok {
				lastContextUsed = info.ContextUsed
				lastContextSize = info.ContextSize
				slog.Debug("S1 context fill from ProcessInfo fallback",
					"station", rc.station, "context_used", lastContextUsed,
					"context_size", lastContextSize)
			}
		}

		shouldReplace, reason := buf.ShouldReplace(lastStopReason, turnErr, lastContextUsed, lastContextSize)

		slog.Debug("S1 exhaustion check",
			"station", rc.station, "session", sessionID,
			"stop_reason", lastStopReason, "turn_err", turnErr,
			"context_used", lastContextUsed, "context_size", lastContextSize,
			"result_len", result.Len(), "should_replace", shouldReplace,
			"reason", reason, "generation", buf.Generation(),
			"turns", len(buf.turns))

		if !shouldReplace {
			return resultIsError, turnErr
		}

		if buf.AtGenerationCap() {
			result.WriteString("\n\n[WARNING: Station reached maximum replacement limit. Result may be incomplete.]")
			ops.KillProcess(ctx, sessionID)
			return resultIsError, turnErr
		}

		slog.Info("Station replaced",
			"station", rc.station, "session", sessionID,
			"generation", buf.Generation()+1, "trigger", reason,
			"turns", len(buf.turns))

		publishReplacementActivity(processStateKey(sessionID, rc.station), sessionID, rc.station, reason)

		if notifier != nil {
			notifier.Send(notify.Notification{
				Kind:   notify.Ephemeral,
				Author: "system",
				Text: fmt.Sprintf("Station %q replaced (gen %d→%d): %s",
					rc.station, buf.Generation(), buf.Generation()+1, reason),
			})
		}

		ops.KillProcess(ctx, sessionID)

		captureFn := rc.captureRepoStateFn
		if captureFn == nil {
			captureFn = captureRepoState
		}
		effectiveCWD := rc.cwd
		if rc.cwdResolver != nil {
			effectiveCWD = rc.cwdResolver(sessionID)
		}
		repoState, repoErr := captureFn(ctx, effectiveCWD)
		buf.SetRepoState(repoState)
		slog.Debug("S1 repo state capture",
			"station", rc.station, "ok", repoErr == nil,
			"len", len(repoState))

		buf.IncrementGeneration()
		result.Reset()
		resultIsError = false

		task = buf.BuildContinuationPrompt()
		slog.Info("S1 continuation prompt",
			"station", rc.station, "generation", buf.Generation(),
			"prompt_len", len(task), "prompt", task)
		resumeID = ""
	}

	return false, fmt.Errorf("station %q: exceeded max replacement iterations", rc.station)
}

// buildWrappedHandler creates a handler that records to ContextBuffer
// while delegating to the UI handler.
func (rc *RecoveryController) buildWrappedHandler(
	buf *ContextBuffer,
	result *strings.Builder,
	handler func(agentrun.Message) error,
	lastStopReason *agentrun.StopReason,
	lastContextUsed *int,
	lastContextSize *int,
	resultIsError *bool,
	ledger *UsageLedger,
) func(agentrun.Message) error {
	recordFill := func(msg agentrun.Message) {
		if used, size, ok := agentrun.ContextFill(msg); ok {
			buf.RecordContextFill(used, size)
			*lastContextUsed = used
			*lastContextSize = size
		}
	}

	return func(msg agentrun.Message) error {
		switch msg.Type {
		case agentrun.MessageText:
			if msg.Tool != nil {
				buf.RecordToolStart(msg.Tool.Name, string(msg.Tool.Input))
			} else {
				buf.AppendResult(msg.Content)
				result.WriteString(msg.Content)
			}
		case agentrun.MessageThinking:
			buf.RecordThinking(msg.Content)
		case agentrun.MessageToolResult:
			buf.RecordToolOutput(msg.Content)
		case agentrun.MessageError:
			buf.RecordError(msg.Content)
		case agentrun.MessageResult:
			*lastStopReason = msg.StopReason
			if msg.IsError {
				*resultIsError = true
			}
			if msg.Content != "" && result.Len() == 0 {
				buf.AppendResult(msg.Content)
				result.WriteString(msg.Content)
			}
			recordFill(msg)
			// Record actual station token usage as unpersisted delta.
			if ledger != nil && msg.Usage != nil {
				cost := msg.Usage.CostUSD
				if cost == 0 {
					cost = computeStationCost(rc.model, msg.Usage)
				}
				ledger.Add(
					int64(msg.Usage.InputTokens),
					int64(msg.Usage.OutputTokens),
					int64(msg.Usage.ThinkingTokens),
					int64(msg.Usage.CacheReadTokens),
					int64(msg.Usage.CacheWriteTokens),
					cost,
				)
			}
		case agentrun.MessageContextWindow:
			recordFill(msg)
		}
		return handler(msg)
	}
}
