package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClient implements registryClient for testing.
type mockClient struct {
	providers []ProviderMetadata
	err       error
	calls     atomic.Int32
}

func (m *mockClient) GetProviders(_ context.Context, _ string) ([]ProviderMetadata, error) {
	m.calls.Add(1)
	return m.providers, m.err
}

func testProviders() []ProviderMetadata {
	return []ProviderMetadata{
		{
			Name: "Test Gemini",
			ID:   "gemini",
			Type: ProviderTypeGemini,
			Models: []ModelMetadata{
				{ID: "gemini-test", Name: "Test Model", ContextWindow: 1_000_000},
			},
		},
	}
}

func TestFetchRegistry_AutoUpdateDisabled(t *testing.T) {
	mock := &mockClient{providers: testProviders()}
	got, err := fetchRegistry(context.Background(), mock, filepath.Join(t.TempDir(), "cache.json"), false)
	require.NoError(t, err)
	assert.Equal(t, embeddedProviders(), got, "should return embedded when autoupdate is disabled")
	assert.Equal(t, int32(0), mock.calls.Load(), "should not call client when autoupdate is disabled")
}

func TestFetchRegistry_FreshProviders(t *testing.T) {
	mock := &mockClient{providers: testProviders()}
	got, err := fetchRegistry(context.Background(), mock, filepath.Join(t.TempDir(), "cache.json"), true)
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got)
	assert.Equal(t, int32(1), mock.calls.Load())
}

func TestFetchRegistry_CachedOnNetworkError(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	// Pre-populate cache.
	c := newCache[[]ProviderMetadata](cachePath)
	require.NoError(t, c.Store(testProviders()))

	mock := &mockClient{err: fmt.Errorf("network down")}
	got, err := fetchRegistry(context.Background(), mock, cachePath, true)
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got, "should return cached on network error")
}

func TestFetchRegistry_EmbeddedOnEmptyCache(t *testing.T) {
	mock := &mockClient{err: fmt.Errorf("network down")}
	got, err := fetchRegistry(context.Background(), mock, filepath.Join(t.TempDir(), "no-cache.json"), true)
	require.NoError(t, err)
	assert.Equal(t, embeddedProviders(), got, "should return embedded when no cache and network fails")
}

func TestFetchRegistry_304NotModified(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	c := newCache[[]ProviderMetadata](cachePath)
	require.NoError(t, c.Store(testProviders()))

	mock := &mockClient{err: ErrNotModified}
	got, err := fetchRegistry(context.Background(), mock, cachePath, true)
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got, "should return cached on 304")
}

func TestFetchRegistry_EmptyResponseReturnsCachedWithoutError(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	c := newCache[[]ProviderMetadata](cachePath)
	require.NoError(t, c.Store(testProviders()))

	mock := &mockClient{providers: []ProviderMetadata{}} // 200 OK but empty
	got, err := fetchRegistry(context.Background(), mock, cachePath, true)
	require.NoError(t, err, "should NOT return an error — cached data is valid fallback")
	assert.Equal(t, testProviders(), got, "should return cached providers on empty response")
}

func TestFetchRegistry_StatelessPerCall(t *testing.T) {
	mock1 := &mockClient{providers: testProviders()}
	mock2 := &mockClient{providers: embeddedProviders()}

	got1, err := fetchRegistry(context.Background(), mock1, filepath.Join(t.TempDir(), "cache1.json"), true)
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got1)

	got2, err := fetchRegistry(context.Background(), mock2, filepath.Join(t.TempDir(), "cache2.json"), true)
	require.NoError(t, err)
	assert.Equal(t, embeddedProviders(), got2, "second call should use its own client, not be frozen by first call")

	assert.Equal(t, int32(1), mock1.calls.Load())
	assert.Equal(t, int32(1), mock2.calls.Load())
}

// UpdateProviders tests using httptest and temp dirs.

func TestUpdateProviders_Embedded(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	require.NoError(t, UpdateProviders("embedded"))

	c := newCache[[]ProviderMetadata](cachePathFor("providers"))
	got, _, err := c.Get()
	require.NoError(t, err)
	assert.Equal(t, embeddedProviders(), got)
}

func TestUpdateProviders_BaseURL(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(testProviders())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	require.NoError(t, UpdateProviders(srv.URL))

	c := newCache[[]ProviderMetadata](cachePathFor("providers"))
	got, _, err := c.Get()
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got)
}

func TestUpdateProviders_LocalFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	data, err := json.Marshal(testProviders())
	require.NoError(t, err)

	filePath := filepath.Join(t.TempDir(), "providers.json")
	require.NoError(t, os.WriteFile(filePath, data, 0o600))

	require.NoError(t, UpdateProviders(filePath))

	c := newCache[[]ProviderMetadata](cachePathFor("providers"))
	got, _, err := c.Get()
	require.NoError(t, err)
	assert.Equal(t, testProviders(), got)
}

func TestUpdateProviders_InvalidFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	filePath := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(filePath, []byte("not json"), 0o600))

	err := UpdateProviders(filePath)
	assert.Error(t, err)
}

func TestUpdateProviders_RejectsEmptyList(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	filePath := filepath.Join(t.TempDir(), "empty.json")
	require.NoError(t, os.WriteFile(filePath, []byte("[]"), 0o600))

	err := UpdateProviders(filePath)
	assert.Error(t, err, "should reject empty provider list")
	assert.Contains(t, err.Error(), "empty provider list")
}

func TestUpdateProviders_RejectsEmptyFromHTTP(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	err := UpdateProviders(srv.URL)
	assert.Error(t, err, "should reject empty provider list from HTTP")
	assert.Contains(t, err.Error(), "empty provider list")
}
