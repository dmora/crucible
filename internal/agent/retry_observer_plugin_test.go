package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmora/adk-go-extras/plugin/notify"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retryResult builds a retryandreflect-style result map.
func retryResult(retryCount int, errorDetails string) map[string]any {
	return map[string]any{
		"response_type":       "ERROR_HANDLED_BY_REFLECT_AND_RETRY_PLUGIN",
		"error_type":          "tool_error",
		"error_details":       errorDetails,
		"retry_count":         retryCount,
		"reflection_guidance": "Try a different approach.",
	}
}

// collectRetryEvents subscribes to processBroker and collects events until ctx is canceled.
func collectRetryEvents(ctx context.Context) <-chan ProcessEvent {
	ch := make(chan ProcessEvent, 10)
	sub := processBroker.Subscribe(ctx)
	go func() {
		defer close(ch)
		for ev := range sub {
			if ev.Payload.Type == ProcessEventRetry || ev.Payload.Type == ProcessEventRetryExhausted {
				ch <- ev.Payload
			}
		}
	}()
	return ch
}

func TestRetryObserver_IgnoresNormalResult(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()

	result, err := obs.afterTool(ctx, fakeTool{name: "build"}, nil, map[string]any{"status": "ok"}, nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestRetryObserver_IgnoresNilResult(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()

	result, err := obs.afterTool(ctx, fakeTool{name: "build"}, nil, nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestRetryObserver_IgnoresErrorPath(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()

	result, err := obs.afterTool(ctx, fakeTool{name: "build"}, nil, retryResult(1, "fail"), errors.New("some error"))
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestRetryObserver_DetectsRetry(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()

	subCtx, cancel := context.WithCancel(context.Background())
	events := collectRetryEvents(subCtx)

	result, err := obs.afterTool(ctx, fakeTool{name: "build"}, nil, retryResult(1, "connection refused"), nil)
	assert.NoError(t, err)
	assert.Nil(t, result)

	cancel()
	var got []ProcessEvent
	for ev := range events {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	assert.Equal(t, ProcessEventRetry, got[0].Type)
	assert.Equal(t, "build", got[0].RetryTool)
	assert.Equal(t, 1, got[0].RetryAttempt)
	assert.Equal(t, 3, got[0].RetryMax)
	assert.Equal(t, "connection refused", got[0].RetryError)
	assert.Equal(t, "session-test", got[0].SessionID)
	assert.Equal(t, "build", got[0].Station)
}

func TestRetryObserver_DetectsExhaustion(t *testing.T) {
	ntf := notify.New(notify.WithMaxBatch(10))
	obs := &retryObserver{notifier: ntf, maxRetries: 3}
	ctx := newMockToolContext()

	subCtx, cancel := context.WithCancel(context.Background())
	events := collectRetryEvents(subCtx)

	result, err := obs.afterTool(ctx, fakeTool{name: "draft"}, nil, retryResult(3, "timeout"), nil)
	assert.NoError(t, err)
	assert.Nil(t, result)

	cancel()
	var got []ProcessEvent
	for ev := range events {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	assert.Equal(t, ProcessEventRetryExhausted, got[0].Type)
	assert.Equal(t, "draft", got[0].RetryTool)
	assert.Equal(t, 3, got[0].RetryAttempt)
	assert.Equal(t, 3, got[0].RetryMax)
	assert.Equal(t, "session-test", got[0].SessionID)
}

func TestRetryObserver_TruncatesLongError(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()
	longErr := strings.Repeat("x", 200)

	subCtx, cancel := context.WithCancel(context.Background())
	events := collectRetryEvents(subCtx)

	_, _ = obs.afterTool(ctx, fakeTool{name: "build"}, nil, retryResult(1, longErr), nil)

	cancel()
	var got []ProcessEvent
	for ev := range events {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	assert.LessOrEqual(t, len(got[0].RetryError), maxRetryErrorExcerpt)
	assert.True(t, strings.HasSuffix(got[0].RetryError, "..."))
}

func TestRetryObserver_NilNotifierSafe(t *testing.T) {
	obs := &retryObserver{notifier: nil, maxRetries: 3}
	ctx := newMockToolContext()

	subCtx, cancel := context.WithCancel(context.Background())
	events := collectRetryEvents(subCtx)

	// Exhaustion with nil notifier should not panic.
	result, err := obs.afterTool(ctx, fakeTool{name: "build"}, nil, retryResult(3, "timeout"), nil)
	assert.NoError(t, err)
	assert.Nil(t, result)

	cancel()
	var got []ProcessEvent
	for ev := range events {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	assert.Equal(t, ProcessEventRetryExhausted, got[0].Type)
}

func TestRetryObserver_ExhaustsWhenCountExceedsMax(t *testing.T) {
	obs := &retryObserver{maxRetries: 3}
	ctx := newMockToolContext()

	subCtx, cancel := context.WithCancel(context.Background())
	events := collectRetryEvents(subCtx)

	// retry_count=5 > maxRetries=3 → exhaustion
	_, _ = obs.afterTool(ctx, fakeTool{name: "build"}, nil, retryResult(5, "still failing"), nil)

	cancel()
	var got []ProcessEvent
	for ev := range events {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	assert.Equal(t, ProcessEventRetryExhausted, got[0].Type)
	assert.Equal(t, 5, got[0].RetryAttempt)
}

func TestIntFromResult(t *testing.T) {
	assert.Equal(t, 0, intFromResult(nil, "x"))
	assert.Equal(t, 0, intFromResult(map[string]any{}, "x"))
	assert.Equal(t, 3, intFromResult(map[string]any{"x": 3}, "x"))
	assert.Equal(t, 3, intFromResult(map[string]any{"x": float64(3)}, "x"))
	assert.Equal(t, 3, intFromResult(map[string]any{"x": int64(3)}, "x"))
	assert.Equal(t, 0, intFromResult(map[string]any{"x": "not-a-number"}, "x"))
}

func TestStringFromResult(t *testing.T) {
	assert.Equal(t, "", stringFromResult(nil, "x"))
	assert.Equal(t, "", stringFromResult(map[string]any{}, "x"))
	assert.Equal(t, "hello", stringFromResult(map[string]any{"x": "hello"}, "x"))
	assert.Equal(t, "", stringFromResult(map[string]any{"x": 42}, "x"))
}

// Verify that the subscriber helper works with the real pubsub broker.
func TestRetryObserver_PubsubIntegration(t *testing.T) {
	obs := &retryObserver{maxRetries: 2}
	ctx := newMockToolContext()

	subCtx, cancel := context.WithCancel(context.Background())
	sub := processBroker.Subscribe(subCtx)

	// Fire a retry event.
	_, _ = obs.afterTool(ctx, fakeTool{name: "verify"}, nil, retryResult(1, "err"), nil)

	ev := <-sub
	cancel()

	assert.Equal(t, pubsub.UpdatedEvent, ev.Type)
	assert.Equal(t, ProcessEventRetry, ev.Payload.Type)
	assert.Equal(t, "verify", ev.Payload.RetryTool)
}
