package chat

import (
	"strings"
	"testing"

	"github.com/dmora/crucible/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestGroundingRender_CompactShowsDomains(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"test query"},
		Sources: make([]message.GroundingSource, 8),
	}
	// 8 sources (above threshold of 5) — should render compact.
	domains := []string{"a.com", "b.com", "c.com", "d.com", "e.com", "f.com", "g.com", "h.com"}
	for i := range gc.Sources {
		gc.Sources[i] = message.GroundingSource{
			Title:  "Title " + domains[i],
			URL:    "https://" + domains[i] + "/page",
			Domain: domains[i],
		}
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	assert.Contains(t, output, "8 sources:")
	assert.Contains(t, output, "a.com")
	assert.Contains(t, output, "h.com")
	// Compact should NOT show numbered markers.
	assert.NotContains(t, output, "[1]")
}

func TestGroundingRender_ExpandedShowsNumberedSources(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"test"},
		Sources: []message.GroundingSource{
			{Title: "First Page", URL: "https://first.com/a", Domain: "first.com"},
			{Title: "Second Page", URL: "https://second.com/b", Domain: "second.com"},
			{Title: "Third Page", URL: "https://third.com/c", Domain: "third.com"},
		},
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	assert.Contains(t, output, "[1]")
	assert.Contains(t, output, "[2]")
	assert.Contains(t, output, "[3]")
	assert.Contains(t, output, "First Page")
	assert.Contains(t, output, "https://first.com/a")
}

func TestGroundingRender_ExpandedShowsSupports(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"q"},
		Sources: []message.GroundingSource{
			{Title: "Source A", URL: "https://a.com", Domain: "a.com"},
			{Title: "Source B", URL: "https://b.com", Domain: "b.com"},
		},
		Supports: []message.GroundingSupport{
			{Text: "The sky is blue", ChunkIndices: []int32{0}},
			{Text: "Water is wet", ChunkIndices: []int32{0, 1}},
		},
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	assert.Contains(t, output, "Grounded claims:")
	assert.Contains(t, output, "The sky is blue")
	assert.Contains(t, output, "[1]")
	assert.Contains(t, output, "Water is wet")
}

func TestGroundingRender_NoSupportsOmitsSection(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"q"},
		Sources: []message.GroundingSource{
			{Title: "A", URL: "https://a.com", Domain: "a.com"},
		},
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	assert.NotContains(t, output, "Grounded claims:")
}

func TestGroundingRender_NoSources(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"some query"},
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	// Should render header only with no crash.
	assert.Contains(t, output, "Google Search")
	assert.NotContains(t, output, "sources:")
}

func TestGroundingRender_LegacyData(t *testing.T) {
	gc := message.GroundingContent{
		Queries: []string{"q"},
		Sources: []message.GroundingSource{
			{Title: "Legacy Title", URL: "https://legacy.com", Domain: ""},
		},
	}

	item := NewGroundingMessageItem(testStyles(), "msg-1", &gc)
	output := item.RawRender(120)

	// Should fall back to Title when Domain is empty.
	lines := strings.Split(output, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Legacy Title") {
			found = true
			break
		}
	}
	assert.True(t, found, "should render legacy data with Title fallback")
}
