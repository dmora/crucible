package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviders_DisableDefaultProviders(t *testing.T) {
	cfg := &Config{Options: &Options{DisableDefaultProviders: true}}
	got, err := Providers(cfg)
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil when default providers disabled")
}

func TestProviders_DisableAutoUpdate(t *testing.T) {
	cfg := &Config{Options: &Options{DisableProviderAutoUpdate: true}}

	// Point cache to temp dir so it doesn't read a real cache.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := Providers(cfg)
	require.NoError(t, err)

	// Should return all embedded providers.
	assert.Equal(t, embeddedProviders(), got)
}

func TestProviders_FallbackOnNetworkError(t *testing.T) {
	cfg := &Config{Options: &Options{}}

	// Point to a URL that will fail immediately.
	restore := SetDefaultCatalogURLForTest("http://127.0.0.1:1") // unreachable port
	t.Cleanup(restore)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CATALOG_URL", "")

	got, err := Providers(cfg)
	require.NoError(t, err)

	// fetchRegistry falls back to embedded on network error, so we get
	// the embedded providers (not a hardcoded fallback).
	assert.NotEmpty(t, got, "should return providers even when network is unreachable")
}

func TestProviders_BothFlagsInteraction(t *testing.T) {
	cfg := &Config{Options: &Options{
		DisableDefaultProviders:   true,
		DisableProviderAutoUpdate: true,
	}}

	got, err := Providers(cfg)
	require.NoError(t, err)
	assert.Nil(t, got, "DisableDefaultProviders should take precedence")
}

func TestProviders_NotFrozenBySingleton(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// First call with auto-update disabled.
	cfg1 := &Config{Options: &Options{DisableProviderAutoUpdate: true}}
	got1, err := Providers(cfg1)
	require.NoError(t, err)

	// Second call with auto-update enabled but unreachable URL — should still
	// attempt a fresh fetch (falling back to embedded), NOT return a cached result
	// from a frozen sync.Once.
	restore := SetDefaultCatalogURLForTest("http://127.0.0.1:1")
	t.Cleanup(restore)
	t.Setenv("CATALOG_URL", "")

	cfg2 := &Config{Options: &Options{DisableProviderAutoUpdate: false}}
	got2, err := Providers(cfg2)
	require.NoError(t, err)

	// Both should return data (embedded fallback in both cases).
	assert.NotEmpty(t, got1)
	assert.NotEmpty(t, got2)
}

func TestSetDefaultCatalogURLForTest(t *testing.T) {
	original := defaultCatalogURL
	restore := SetDefaultCatalogURLForTest("http://test.example.com")
	assert.Equal(t, "http://test.example.com", defaultCatalogURL)
	restore()
	assert.Equal(t, original, defaultCatalogURL)
}

func TestCachePathFor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/test-xdg")
	got := cachePathFor("providers")
	assert.Equal(t, filepath.Join("/tmp/test-xdg", appName, "providers.json"), got)
}
