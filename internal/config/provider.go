package config

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// defaultCatalogURL is the URL for the model catalog.
// It is a var (not const) so tests can override it without hitting the network.
var defaultCatalogURL = "https://raw.githubusercontent.com/dmora/crucible/main/models.json"

// SetDefaultCatalogURLForTest overrides the default catalog URL and returns
// a restore function. Exported for cross-package test use (cmd tests).
func SetDefaultCatalogURLForTest(url string) func() {
	prev := defaultCatalogURL
	defaultCatalogURL = url
	return func() { defaultCatalogURL = prev }
}

// Providers returns the list of known providers from the model catalog.
// Falls back to the embedded snapshot on catalog errors.
//
// This function is stateless — each call resolves config, fetches, and returns
// fresh results. Called once at startup from Load().
func Providers(cfg *Config) ([]ProviderMetadata, error) {
	if cfg.Options.DisableDefaultProviders {
		return nil, nil
	}

	autoupdate := !cfg.Options.DisableProviderAutoUpdate
	url := cmp.Or(os.Getenv("CATALOG_URL"), defaultCatalogURL)
	client := newCatalogClient(url, &http.Client{Timeout: 15 * time.Second})
	cachePath := cachePathFor("providers")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	providers, err := fetchRegistry(ctx, client, cachePath, autoupdate)
	if err != nil {
		slog.Warn("Failed to get providers, using embedded fallback", "err", err)
		return embeddedProviders(), nil
	}

	return providers, nil
}
