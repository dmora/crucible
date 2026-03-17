package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmora/crucible/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCmdProviders() []config.ProviderMetadata {
	return []config.ProviderMetadata{
		{
			Name: "Test Gemini",
			ID:   "gemini",
			Type: config.ProviderTypeGemini,
			Models: []config.ModelMetadata{
				{ID: "gemini-test", Name: "Test Model", ContextWindow: 1_000_000},
			},
		},
	}
}

func TestUpdateProvidersCmd_Embedded(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	rootCmd.SetArgs([]string{"update-providers", "embedded"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify cache was written.
	cachePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "crucible", "providers.json")
	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var got []config.ProviderMetadata
	require.NoError(t, json.Unmarshal(data, &got))
	assert.NotEmpty(t, got, "embedded providers should not be empty")
}

func TestUpdateProvidersCmd_BaseURL(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(testCmdProviders())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	rootCmd.SetArgs([]string{"update-providers", srv.URL})
	err := rootCmd.Execute()
	require.NoError(t, err)

	cachePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "crucible", "providers.json")
	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var got []config.ProviderMetadata
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, testCmdProviders(), got)
}

func TestUpdateProvidersCmd_LocalFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	data, err := json.Marshal(testCmdProviders())
	require.NoError(t, err)

	filePath := filepath.Join(t.TempDir(), "providers.json")
	require.NoError(t, os.WriteFile(filePath, data, 0o600))

	rootCmd.SetArgs([]string{"update-providers", filePath})
	err = rootCmd.Execute()
	require.NoError(t, err)

	cachePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "crucible", "providers.json")
	cacheData, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var got []config.ProviderMetadata
	require.NoError(t, json.Unmarshal(cacheData, &got))
	assert.Equal(t, testCmdProviders(), got)
}

func TestUpdateProvidersCmd_NoArgs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(testCmdProviders())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	restore := config.SetDefaultCatalogURLForTest(srv.URL)
	defer restore()

	rootCmd.SetArgs([]string{"update-providers"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	cachePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "crucible", "providers.json")
	_, err = os.Stat(cachePath)
	assert.NoError(t, err, "cache file should exist")
}
