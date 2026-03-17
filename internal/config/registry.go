package config

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// registryClient abstracts the catalog HTTP client for testing.
type registryClient interface {
	GetProviders(ctx context.Context, etag string) ([]ProviderMetadata, error)
}

// fetchRegistry fetches providers from the model catalog with a fallback chain:
//  1. !autoupdate → embedded snapshot
//  2. Load cache + ETag; on miss → embedded as cached baseline
//  3. Network fetch with ETag
//  4. On 304 / error / empty → cached
//  5. On success → store + return fresh
//
// This is a pure function — no package-level state, no sync.Once.
func fetchRegistry(ctx context.Context, client registryClient, cachePath string, autoupdate bool) ([]ProviderMetadata, error) {
	if !autoupdate {
		slog.Debug("Registry auto-update disabled, using embedded providers")
		return embeddedProviders(), nil
	}

	c := newCache[[]ProviderMetadata](cachePath)

	// Load from disk cache; fall back to embedded if missing/corrupt.
	cached, etagVal, err := c.Get()
	if err != nil || len(cached) == 0 {
		cached = embeddedProviders()
		etagVal = ""
	}

	providers, err := client.GetProviders(ctx, etagVal)
	switch {
	case errors.Is(err, ErrNotModified):
		slog.Debug("Registry not modified (304), using cached")
		return cached, nil
	case err != nil:
		slog.Warn("Registry fetch failed, using cached providers", "err", err)
		return cached, nil
	case len(providers) == 0:
		// Server returned 200 but empty list. Fall back to cached
		// providers without propagating an error — cached data is valid.
		slog.Warn("Registry returned empty list, using cached providers")
		return cached, nil
	}

	// Success — persist and return fresh data.
	if storeErr := c.Store(providers); storeErr != nil {
		slog.Warn("Failed to cache providers", "err", storeErr)
	}
	return providers, nil
}

// UpdateProviders fetches providers from the given source and writes them
// to the local cache. Used by the CLI update-providers command.
//
// pathOrURL can be:
//   - "" → fetch from default URL (or CATALOG_URL env)
//   - "embedded" → use embedded snapshot
//   - "http://..." or "https://..." → fetch from URL
//   - anything else → treat as local file path
func UpdateProviders(pathOrURL string) error {
	resolved := cmp.Or(pathOrURL, os.Getenv("CATALOG_URL"), defaultCatalogURL)
	c := newCache[[]ProviderMetadata](cachePathFor("providers"))

	var providers []ProviderMetadata

	switch {
	case resolved == "embedded":
		providers = embeddedProviders()

	case strings.HasPrefix(resolved, "http://") || strings.HasPrefix(resolved, "https://"):
		client := newCatalogClient(resolved, &http.Client{Timeout: 15 * time.Second})
		var err error
		providers, err = client.GetProviders(context.Background(), "")
		if err != nil {
			return fmt.Errorf("fetching providers from %s: %w", resolved, err)
		}

	default:
		data, err := os.ReadFile(resolved) //nolint:gosec // user-provided path is expected for CLI command
		if err != nil {
			return fmt.Errorf("reading providers file %s: %w", resolved, err)
		}
		if err := json.Unmarshal(data, &providers); err != nil {
			return fmt.Errorf("parsing providers file %s: %w", resolved, err)
		}
	}

	// Reject empty provider lists — prevents caching data that the
	// runtime would treat as a cache miss (len(cached)==0 falls through to embedded).
	if len(providers) == 0 {
		return fmt.Errorf("refusing to cache empty provider list from %s", resolved)
	}

	return c.Store(providers)
}
