package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_StoreAndGet(t *testing.T) {
	dir := t.TempDir()
	c := newCache[[]string](filepath.Join(dir, "test.json"))

	input := []string{"alpha", "bravo", "charlie"}
	require.NoError(t, c.Store(input))

	got, etag, err := c.Get()
	require.NoError(t, err)
	assert.Equal(t, input, got)
	assert.NotEmpty(t, etag, "ETag should be non-empty")

	// Store same data again — ETag should be stable.
	require.NoError(t, c.Store(input))
	_, etag2, err := c.Get()
	require.NoError(t, err)
	assert.Equal(t, etag, etag2, "ETag should be stable for same data")
}

func TestCache_GetNonExistent(t *testing.T) {
	c := newCache[[]string](filepath.Join(t.TempDir(), "does-not-exist.json"))
	val, etag, err := c.Get()
	assert.Error(t, err)
	assert.Nil(t, val)
	assert.Empty(t, etag)
}

func TestCache_StoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	c := newCache[map[string]int](filepath.Join(dir, "data.json"))

	require.NoError(t, c.Store(map[string]int{"x": 42}))

	_, err := os.Stat(filepath.Join(dir, "data.json"))
	assert.NoError(t, err, "cache file should exist after Store")
}
