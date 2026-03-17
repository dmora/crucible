package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dmora/crucible/internal/home"
)

// cache is a generic file-backed JSON cache with content-hash ETag support.
type cache[T any] struct {
	path string
}

func newCache[T any](path string) cache[T] {
	return cache[T]{path: path}
}

// Get reads the cached value and returns it along with a content-hash ETag.
// Returns the zero value and an error if the file doesn't exist or is invalid.
func (c cache[T]) Get() (T, string, error) {
	var zero T
	data, err := os.ReadFile(c.path)
	if err != nil {
		return zero, "", fmt.Errorf("reading cache: %w", err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, "", fmt.Errorf("unmarshaling cache: %w", err)
	}
	return v, catalogEtag(data), nil
}

// Store writes the value to the cache file as JSON.
func (c cache[T]) Store(v T) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0o600); err != nil {
		return fmt.Errorf("writing cache: %w", err)
	}
	return nil
}

// cachePathFor returns the XDG-aware path to ~/.local/share/crucible/<name>.json.
func cachePathFor(name string) string {
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName, name+".json")
	}
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, appName, name+".json")
	}
	return filepath.Join(home.Dir(), ".local", "share", appName, name+".json")
}
