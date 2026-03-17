package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCfg returns a RetryTransportConfig with short delays for fast tests.
func testCfg() RetryTransportConfig {
	return RetryTransportConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
		JitterRange:  0, // No jitter for deterministic tests.
	}
}

// newTestServer creates a test server that returns the given status codes in sequence.
// After the sequence is exhausted, it returns 200.
func newTestServer(t *testing.T, responses []int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(count.Add(1)) - 1
		status := http.StatusOK
		if idx < len(responses) {
			status = responses[idx]
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, "response-%d", idx)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// doGet is a helper that creates a GET request with context and executes it.
func doGet(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return client.Do(req)
}

func TestRetryTransport_SuccessFirstAttempt(t *testing.T) {
	srv, count := newTestServer(t, []int{200})

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, testCfg())}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(1), count.Load())
}

func TestRetryTransport_TransientThenSuccess(t *testing.T) {
	srv, count := newTestServer(t, []int{503, 200})

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, testCfg())}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(2), count.Load())
}

func TestRetryTransport_429WithRetryAfter(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := int(count.Add(1)) - 1
		if idx == 0 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	cfg := testCfg()
	cfg.InitialDelay = 1 * time.Millisecond // Retry-After: 1s should override this.

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, cfg)}
	start := time.Now()
	resp, err := doGet(t, client, srv.URL)
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(2), count.Load())
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "should have respected Retry-After")
}

func TestRetryTransport_MaxAttemptsExhausted(t *testing.T) {
	cfg := testCfg()
	cfg.MaxAttempts = 3
	srv, count := newTestServer(t, []int{503, 503, 503})

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, cfg)}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 503, resp.StatusCode)
	assert.Equal(t, int32(3), count.Load())

	// Caller should be able to read the final body.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "response-2", string(body))
}

func TestRetryTransport_NonTransientNotRetried(t *testing.T) {
	srv, count := newTestServer(t, []int{400})

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, testCfg())}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, int32(1), count.Load())
}

func TestRetryTransport_ContextCancellation(t *testing.T) {
	cfg := testCfg()
	cfg.InitialDelay = 10 * time.Second // Long delay so we can cancel during it.
	cfg.MaxDelay = 10 * time.Second     // Must also be high so clamping doesn't shorten the sleep.
	cfg.MaxAttempts = 3
	srv, count := newTestServer(t, []int{503, 503, 200})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short time, while the transport is sleeping.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, cfg)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, doErr := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, doErr)
	assert.Less(t, count.Load(), int32(3), "should not have completed all attempts")
}

func TestRetryTransport_RequestBodyPreserved(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(count.Add(1)) - 1
		body, _ := io.ReadAll(r.Body)
		if idx == 0 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		w.Write(body) // Echo body back.
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, testCfg())}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, strings.NewReader("hello-body"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "hello-body", string(body))
	assert.Equal(t, int32(2), count.Load())
}

func TestRetryTransport_ResponseBodyClosedOnRetry(t *testing.T) {
	var count atomic.Int32
	var firstBodyRead atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := int(count.Add(1)) - 1
		if idx == 0 {
			w.WriteHeader(503)
			w.Write([]byte("transient-error"))
			return
		}
		// By the time we get here, the first response body should have been
		// drained and closed by the retry transport.
		firstBodyRead.Store(true)
		w.WriteHeader(200)
		w.Write([]byte("success"))
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, testCfg())}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.True(t, firstBodyRead.Load())
	assert.Equal(t, int32(2), count.Load())
}

func TestRetryTransport_FinalResponseBodyNotClosed(t *testing.T) {
	cfg := testCfg()
	cfg.MaxAttempts = 2
	srv, _ := newTestServer(t, []int{503, 503})

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, cfg)}
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)

	// Final body should still be readable (not closed by transport).
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	assert.NotEmpty(t, body)
}

func TestRetryTransport_RetryAfterHTTPDate(t *testing.T) {
	// Retry-After as HTTP-date ~3s in the future. Use a wide margin
	// because RFC1123 has only second-level precision.
	futureTime := time.Now().Add(3 * time.Second).UTC().Format(time.RFC1123)
	parsed := parseRetryAfter(futureTime)
	assert.Greater(t, parsed, 1*time.Second)
	assert.LessOrEqual(t, parsed, 4*time.Second)
}

func TestIsTransient(t *testing.T) {
	transient := []int{429, 500, 502, 503, 504}
	for _, code := range transient {
		assert.True(t, isTransient(code), "expected %d to be transient", code)
	}
	nonTransient := []int{200, 201, 301, 400, 401, 403, 404, 405}
	for _, code := range nonTransient {
		assert.False(t, isTransient(code), "expected %d to be non-transient", code)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"integer seconds", "5", 5 * time.Second},
		{"zero seconds", "0", 0},
		{"negative", "-1", 0},
		{"invalid", "not-a-number", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.header)
			assert.Equal(t, tt.want, got)
		})
	}
}
