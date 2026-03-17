package agent

import (
	"errors"
	"log/slog"
	"time"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/genai"
)

// retryPlugin intercepts transient API errors (429, 500, 502, 503, 504) from
// the model layer and retries with exponential backoff. This handles SSE-wrapped
// errors that bypass the HTTP-level retryTransport (e.g. Vertex AI returning
// HTTP 200 then signaling 429 RESOURCE_EXHAUSTED inside the stream).
type retryPlugin struct {
	model adkmodel.LLM
	cfg   RetryTransportConfig
}

// newRetryPlugin creates an ADK plugin that retries transient model errors.
func newRetryPlugin(m adkmodel.LLM, cfg RetryTransportConfig) *plugin.Plugin {
	p := &retryPlugin{model: m, cfg: cfg}
	plug, _ := plugin.New(plugin.Config{
		Name:                 "crucible_retry",
		OnModelErrorCallback: p.onModelError,
	})
	return plug
}

// onModelError handles transient API errors by retrying the model call.
func (p *retryPlugin) onModelError(
	ctx adkagent.CallbackContext,
	req *adkmodel.LLMRequest,
	llmErr error,
) (*adkmodel.LLMResponse, error) {
	var apiErr genai.APIError
	if !errors.As(llmErr, &apiErr) {
		return nil, nil // not an API error — let ADK handle it
	}

	if !isTransient(apiErr.Code) {
		return nil, nil // non-transient — let ADK handle it
	}

	// Retry loop: MaxAttempts-1 retries (original call was attempt 0).
	var lastErr error
	for attempt := range p.cfg.MaxAttempts - 1 {
		delay := computeBackoff(p.cfg, attempt)

		slog.Warn("Retrying model call after transient API error",
			"attempt", attempt+1,
			"code", apiErr.Code,
			"delay_ms", delay.Milliseconds(),
		)

		// Context-aware sleep.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}

		// Retry the model call (non-streaming).
		for resp, err := range p.model.GenerateContent(ctx, req, false) {
			if err != nil {
				var retryAPIErr genai.APIError
				if errors.As(err, &retryAPIErr) && isTransient(retryAPIErr.Code) {
					lastErr = err
					apiErr = retryAPIErr // update for next log message
					break                // continue outer retry loop
				}
				return nil, err // non-transient error: replace original
			}
			return resp, nil // success: replace error with response
		}
		_ = lastErr // used to continue retry loop
	}

	return nil, nil // exhausted retries — let original error propagate
}
