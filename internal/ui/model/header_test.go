package model

import (
	"testing"
)

func TestHeaderInvalidateCache(t *testing.T) {
	h := &header{
		logo:        "cached-logo",
		compactLogo: "cached-compact",
		width:       120,
		compact:     true,
	}

	h.InvalidateCache()

	if h.logo != "" {
		t.Errorf("logo not cleared: got %q", h.logo)
	}
	if h.compactLogo != "" {
		t.Errorf("compactLogo not cleared: got %q", h.compactLogo)
	}
	if h.width != 0 {
		t.Errorf("width not reset: got %d", h.width)
	}
	if h.compact {
		t.Error("compact not reset to false")
	}
}
