package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"log/slog"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test infrastructure ---

type mockLLMResult struct {
	resp *adkmodel.LLMResponse
	err  error
}

type mockLLM struct {
	calls   int
	results []mockLLMResult
}

func (m *mockLLM) Name() string { return "mock-llm" }

func (m *mockLLM) GenerateContent(_ context.Context, _ *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	idx := m.calls
	m.calls++
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if idx < len(m.results) {
			yield(m.results[idx].resp, m.results[idx].err)
		}
	}
}

func testRetryCfg() RetryTransportConfig {
	return RetryTransportConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
		JitterRange:  0,
	}
}

func transientErr(code int) error {
	return genai.APIError{Code: code, Status: "RESOURCE_EXHAUSTED", Message: "try later"}
}

func okResponse() *adkmodel.LLMResponse {
	return &adkmodel.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{{Text: "ok"}},
		},
	}
}

// --- tests ---

func TestRetryPlugin_TransientAPIErrorTriggersRetry(t *testing.T) {
	m := &mockLLM{
		results: []mockLLMResult{
			{resp: okResponse(), err: nil},
		},
	}
	p := &retryPlugin{model: m, cfg: testRetryCfg()}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(429))

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "ok", resp.Content.Parts[0].Text)
	assert.Equal(t, 1, m.calls, "model should be called exactly once for the retry")
}

func TestRetryPlugin_NonTransientAPIErrorPassesThrough(t *testing.T) {
	m := &mockLLM{}
	p := &retryPlugin{model: m, cfg: testRetryCfg()}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, genai.APIError{Code: 400, Message: "bad request"})

	assert.Nil(t, resp)
	assert.Nil(t, err)
	assert.Equal(t, 0, m.calls, "model should not be called for non-transient errors")
}

func TestRetryPlugin_NonAPIErrorPassesThrough(t *testing.T) {
	m := &mockLLM{}
	p := &retryPlugin{model: m, cfg: testRetryCfg()}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, errors.New("connection reset"))

	assert.Nil(t, resp)
	assert.Nil(t, err)
	assert.Equal(t, 0, m.calls, "model should not be called for non-API errors")
}

func TestRetryPlugin_MaxRetriesExhausted(t *testing.T) {
	m := &mockLLM{
		results: []mockLLMResult{
			{err: transientErr(503)},
			{err: transientErr(503)},
		},
	}
	cfg := testRetryCfg()
	cfg.MaxAttempts = 3 // original + 2 retries
	p := &retryPlugin{model: m, cfg: cfg}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(503))

	assert.Nil(t, resp)
	assert.Nil(t, err, "should return nil to let original error propagate")
	assert.Equal(t, 2, m.calls, "model should be called MaxAttempts-1 times")
}

func TestRetryPlugin_ContextCancelledDuringBackoff(t *testing.T) {
	m := &mockLLM{}
	cfg := testRetryCfg()
	cfg.InitialDelay = 10 * time.Second // long delay to ensure cancel fires first
	cfg.MaxDelay = 10 * time.Second

	p := &retryPlugin{model: m, cfg: cfg}

	cancelCtx, cancel := context.WithCancel(context.Background())
	ctx := &mockCallbackContext{Context: cancelCtx, sessionID: "s1", invocationID: "inv-1"}

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(429))
	elapsed := time.Since(start)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 200*time.Millisecond, "should cancel quickly, not wait for full backoff")
	assert.Equal(t, 0, m.calls, "model should not be called after cancel")
}

func TestRetryPlugin_BackoffDelayIncreases(t *testing.T) {
	m := &mockLLM{
		results: []mockLLMResult{
			{err: transientErr(429)},
			{err: transientErr(429)},
			{resp: okResponse()},
		},
	}
	cfg := RetryTransportConfig{
		MaxAttempts:  4,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		JitterRange:  0,
	}
	p := &retryPlugin{model: m, cfg: cfg}
	ctx := newMockCtx("s1")

	start := time.Now()
	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(429))
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)
	// Delays: 100ms (attempt 0) + 200ms (attempt 1) + 400ms (attempt 2) = 700ms total
	assert.GreaterOrEqual(t, elapsed, 700*time.Millisecond, "total delay should be >= 700ms")
	assert.Less(t, elapsed, 1500*time.Millisecond, "should not take excessively long")
	assert.Equal(t, 3, m.calls)
}

func TestRetryPlugin_MixedErrors(t *testing.T) {
	nonTransientErr := genai.APIError{Code: 400, Message: "bad request"}
	m := &mockLLM{
		results: []mockLLMResult{
			{err: transientErr(503)}, // retry 1: transient → continue
			{err: nonTransientErr},   // retry 2: non-transient → stop
		},
	}
	cfg := testRetryCfg()
	cfg.MaxAttempts = 4
	p := &retryPlugin{model: m, cfg: cfg}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(429))

	assert.Nil(t, resp)
	require.Error(t, err)
	var apiErr genai.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, 400, apiErr.Code, "should return the non-transient error")
	assert.Equal(t, 2, m.calls)
}

func TestComputeBackoff_ConsistentWithTransport(t *testing.T) {
	cfg := RetryTransportConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		JitterRange:  0, // deterministic
	}
	rt := &retryTransport{cfg: cfg}

	for attempt := range 5 {
		got := computeBackoff(cfg, attempt)
		want := rt.backoffDelay(attempt, "") // no Retry-After header
		assert.Equal(t, want, got, "attempt %d: computeBackoff should match backoffDelay", attempt)
	}
}

func TestRetryPlugin_LogsRetryAttempts(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldDefault)

	m := &mockLLM{
		results: []mockLLMResult{
			{err: transientErr(503)},
			{resp: okResponse()},
		},
	}
	cfg := testRetryCfg()
	cfg.MaxAttempts = 4
	p := &retryPlugin{model: m, cfg: cfg}
	ctx := newMockCtx("s1")

	resp, err := p.onModelError(ctx, &adkmodel.LLMRequest{}, transientErr(429))
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Parse JSON log lines.
	type logEntry struct {
		Level   string `json:"level"`
		Msg     string `json:"msg"`
		Attempt int    `json:"attempt"`
		Code    int    `json:"code"`
		DelayMs int64  `json:"delay_ms"`
	}

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 2, "should have 2 WARN log entries (one per retry attempt)")

	var entry1, entry2 logEntry
	require.NoError(t, json.Unmarshal(lines[0], &entry1))
	require.NoError(t, json.Unmarshal(lines[1], &entry2))

	assert.Equal(t, "WARN", entry1.Level)
	assert.Equal(t, "Retrying model call after transient API error", entry1.Msg)
	assert.Equal(t, 1, entry1.Attempt)
	assert.Equal(t, 429, entry1.Code)

	assert.Equal(t, "WARN", entry2.Level)
	assert.Equal(t, 2, entry2.Attempt)
	assert.Equal(t, 503, entry2.Code, "second log should reflect the updated error code")
}
