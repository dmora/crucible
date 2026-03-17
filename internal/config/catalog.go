package config

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/charmbracelet/x/etag"
)

//go:embed models.json
var embeddedCatalog []byte

// ErrNotModified is returned when the remote catalog has not changed (HTTP 304).
var ErrNotModified = errors.New("not modified")

// embeddedProviders unmarshals the compile-time embedded models.json.
func embeddedProviders() []ProviderMetadata {
	var providers []ProviderMetadata
	if err := json.Unmarshal(embeddedCatalog, &providers); err != nil {
		// Embedded data is a build-time invariant — panic is appropriate.
		panic(fmt.Sprintf("catalog: failed to unmarshal embedded models.json: %v", err))
	}
	return providers
}

// catalogEtag returns a content-hash ETag for the given data.
func catalogEtag(data []byte) string {
	return etag.Of(data)
}

// catalogClient fetches provider metadata from a remote URL.
type catalogClient struct {
	url    string
	client *http.Client
}

// newCatalogClient creates a catalogClient that fetches from the given URL.
func newCatalogClient(url string, client *http.Client) *catalogClient {
	return &catalogClient{url: url, client: client}
}

// GetProviders fetches providers from the remote URL.
// If etagValue is non-empty, sends If-None-Match for conditional GET.
// Returns ErrNotModified on HTTP 304.
func (c *catalogClient) GetProviders(ctx context.Context, etagValue string) ([]ProviderMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil) //nolint:gosec // URL from config/env, not arbitrary user input
	if err != nil {
		return nil, fmt.Errorf("catalog: creating request: %w", err)
	}
	if etagValue != "" {
		etag.Request(req, etagValue)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: fetching %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: unexpected status %d from %s", resp.StatusCode, c.url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("catalog: reading response: %w", err)
	}

	var providers []ProviderMetadata
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, fmt.Errorf("catalog: decoding response: %w", err)
	}
	return providers, nil
}
