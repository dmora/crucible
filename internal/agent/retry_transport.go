package agent

import (
	"bytes"
	"io"
	"log/slog"
	"math"
	"math/rand/v2" //nolint:gosec // jitter for retry backoff, not security-sensitive
	"net/http"
	"strconv"
	"time"
)

// RetryTransportConfig holds retry behavior settings.
type RetryTransportConfig struct {
	MaxAttempts  int           // Total attempts (including first). Default 5.
	InitialDelay time.Duration // Base delay before first retry. Default 1s.
	MaxDelay     time.Duration // Ceiling for backoff delay. Default 60s.
	Multiplier   float64       // Exponential backoff multiplier. Default 2.0.
	JitterRange  time.Duration // Random jitter added to delay (±). Default 1s.
}

// DefaultRetryTransportConfig returns production-appropriate retry settings.
func DefaultRetryTransportConfig() RetryTransportConfig {
	return RetryTransportConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		JitterRange:  1 * time.Second,
	}
}

// retryTransport wraps an http.RoundTripper with retry logic for transient HTTP errors.
type retryTransport struct {
	base http.RoundTripper
	cfg  RetryTransportConfig
}

// NewRetryTransport wraps base with exponential-backoff retry for transient HTTP errors
// (429, 500, 502, 503, 504). Network errors are NOT retried.
func NewRetryTransport(base http.RoundTripper, cfg RetryTransportConfig) http.RoundTripper {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	return &retryTransport{base: base, cfg: cfg}
}

// RoundTrip implements http.RoundTripper with retry logic.
func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := rt.bufferRequestBody(req); err != nil {
		return nil, err
	}

	var resp *http.Response
	var err error

	for attempt := range rt.cfg.MaxAttempts {
		if err := rt.resetRequestBody(req, attempt); err != nil {
			return nil, err
		}

		resp, err = rt.base.RoundTrip(req)
		if err != nil {
			return resp, err // Network errors are not retried.
		}

		if !isTransient(resp.StatusCode) || attempt == rt.cfg.MaxAttempts-1 {
			return resp, nil
		}

		delay := rt.backoffDelay(attempt, resp.Header.Get("Retry-After"))
		rt.logRetry(attempt, resp.StatusCode, delay)

		// Drain and close intermediate response body to release the connection.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if err := rt.waitOrCancel(req, delay); err != nil {
			return nil, err
		}
	}

	return resp, err
}

// bufferRequestBody captures the request body so it can be replayed on retries.
func (rt *retryTransport) bufferRequestBody(req *http.Request) error {
	if req.Body == nil || req.GetBody != nil {
		return nil
	}
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}
	return nil
}

// resetRequestBody resets the request body for retry attempts after the first.
func (rt *retryTransport) resetRequestBody(req *http.Request, attempt int) error {
	if attempt == 0 || req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

// logRetry logs a retry attempt at WARN level.
func (rt *retryTransport) logRetry(attempt, statusCode int, delay time.Duration) {
	slog.Warn("Retrying API request", //nolint:gosec // all values are internally computed, not user input
		"attempt", attempt+1,
		"max_attempts", rt.cfg.MaxAttempts,
		"status", statusCode,
		"delay_ms", delay.Milliseconds(),
	)
}

// waitOrCancel sleeps for the given delay or returns an error if the context is canceled.
func (rt *retryTransport) waitOrCancel(req *http.Request, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return req.Context().Err()
	case <-timer.C:
		return nil
	}
}

// computeBackoff calculates exponential backoff delay for the given attempt.
// Shared by retryTransport (HTTP-level) and retryPlugin (model-level).
func computeBackoff(cfg RetryTransportConfig, attempt int) time.Duration {
	delay := time.Duration(float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt)))

	// Add jitter: uniform random in [-jitterRange, +jitterRange].
	if cfg.JitterRange > 0 {
		jitter := time.Duration(rand.Int64N(int64(2*cfg.JitterRange+1))) - cfg.JitterRange //nolint:gosec // backoff jitter
		delay += jitter
	}

	// Clamp to [0, maxDelay].
	delay = max(delay, 0)
	delay = min(delay, cfg.MaxDelay)

	return delay
}

// backoffDelay computes the delay for the given attempt, respecting Retry-After.
func (rt *retryTransport) backoffDelay(attempt int, retryAfter string) time.Duration {
	delay := computeBackoff(rt.cfg, attempt)

	// Respect Retry-After header as a floor.
	if ra := parseRetryAfter(retryAfter); ra > delay {
		delay = ra
	}

	return delay
}

// isTransient returns true for HTTP status codes that warrant a retry.
func isTransient(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// parseRetryAfter extracts a duration from the Retry-After header.
// Tries integer seconds first, then HTTP-date format. Returns 0 if unparseable.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	// Try integer seconds.
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date (RFC 1123).
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
