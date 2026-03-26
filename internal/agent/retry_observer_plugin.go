package agent

import (
	"fmt"
	"log/slog"

	"github.com/dmora/adk-go-extras/plugin/notify"
	"github.com/dmora/crucible/internal/pubsub"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/tool"
)

// Response marker from retryandreflect — defined locally to avoid import coupling.
// Source: google.golang.org/adk/plugin/retryandreflect
const retryReflectResponseType = "ERROR_HANDLED_BY_REFLECT_AND_RETRY_PLUGIN"

// maxRetryErrorExcerpt is the maximum length of the error excerpt in retry events.
const maxRetryErrorExcerpt = 120

// retryObserver watches for retryandreflect plugin responses and bridges them
// to the UI (via processBroker) and the model (via notifier, on exhaustion only).
type retryObserver struct {
	notifier   *notify.Notifier
	maxRetries int // must match retryandreflect.WithMaxRetries() value
}

// newRetryObserverPlugin creates a plugin that observes retryandreflect responses
// and publishes retry/exhaustion events to the process broker.
func newRetryObserverPlugin(notifier *notify.Notifier, maxRetries int) *plugin.Plugin {
	obs := &retryObserver{notifier: notifier, maxRetries: maxRetries}
	plug, err := plugin.New(plugin.Config{
		Name:              "crucible_retry_observer",
		AfterToolCallback: obs.afterTool,
	})
	if err != nil {
		panic(fmt.Sprintf("crucible_retry_observer: %v", err))
	}
	return plug
}

func (o *retryObserver) afterTool(
	ctx tool.Context,
	t tool.Tool,
	_ map[string]any,
	result map[string]any,
	err error,
) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	// Fast path: check for retryandreflect's response marker.
	rt, ok := result["response_type"]
	if !ok {
		return nil, nil
	}
	rtStr, ok := rt.(string)
	if !ok || rtStr != retryReflectResponseType {
		return nil, nil
	}

	sessionID := ctx.SessionID()
	toolName := t.Name()
	attempt := intFromResult(result, "retry_count")
	errExcerpt := truncate(stringFromResult(result, "error_details"), maxRetryErrorExcerpt)
	exhausted := attempt >= o.maxRetries

	if exhausted {
		o.onExhausted(sessionID, toolName, attempt, errExcerpt)
	} else {
		o.onRetry(sessionID, toolName, attempt, errExcerpt)
	}

	return nil, nil // observe only — never modify the result
}

func (o *retryObserver) onRetry(sessionID, toolName string, attempt int, errExcerpt string) {
	slog.Warn("Tool retry observed",
		"session", sessionID, "tool", toolName, "attempt", attempt, "max", o.maxRetries, "error", errExcerpt)
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:         ProcessEventRetry,
		SessionID:    sessionID,
		Station:      toolName,
		RetryTool:    toolName,
		RetryAttempt: attempt,
		RetryMax:     o.maxRetries,
		RetryError:   errExcerpt,
	})
}

func (o *retryObserver) onExhausted(sessionID, toolName string, attempt int, errExcerpt string) {
	slog.Error("Tool retries exhausted",
		"session", sessionID, "tool", toolName, "max", o.maxRetries, "error", errExcerpt)
	processBroker.Publish(pubsub.UpdatedEvent, ProcessEvent{
		Type:         ProcessEventRetryExhausted,
		SessionID:    sessionID,
		Station:      toolName,
		RetryTool:    toolName,
		RetryAttempt: attempt,
		RetryMax:     o.maxRetries,
		RetryError:   errExcerpt,
	})
	if o.notifier != nil {
		o.notifier.Send(notify.Notification{
			Kind:   notify.Ephemeral,
			Author: "system",
			Text: fmt.Sprintf(
				"Tool %q failed %d times — retries exhausted. Consider alternative approaches or different tool parameters.",
				toolName, o.maxRetries),
		})
	}
}

// intFromResult safely extracts an int from a map[string]any.
func intFromResult(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// stringFromResult safely extracts a string from a map[string]any.
func stringFromResult(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
