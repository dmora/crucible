package dialog

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/ui/list"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

const maxPreviewChars = 80

// ArtifactEntry holds loaded artifact data for display.
type ArtifactEntry struct {
	Name    string
	Station string
	Type    string
	Content string
}

// ArtifactItem wraps an ArtifactEntry to implement the ListItem interface.
type ArtifactItem struct {
	entry   ArtifactEntry
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var _ ListItem = &ArtifactItem{}

// ID returns the artifact name as unique identifier.
func (a *ArtifactItem) ID() string {
	return a.entry.Name
}

// Filter returns the filterable value combining type, station, and first line of content.
func (a *ArtifactItem) Filter() string {
	return a.entry.Type + " " + a.entry.Station + " " + firstLine(a.entry.Content)
}

// SetMatch sets the fuzzy match and clears the render cache.
func (a *ArtifactItem) SetMatch(m fuzzy.Match) {
	a.cache = nil
	a.m = m
}

// SetFocused sets the focus state and clears the render cache on change.
func (a *ArtifactItem) SetFocused(focused bool) {
	if a.focused != focused {
		a.cache = nil
	}
	a.focused = focused
}

// Render returns the string representation of the artifact item.
// Two-line layout: type chip + station name on first line, content preview on second.
func (a *ArtifactItem) Render(width int) string {
	if cached, ok := a.cache[width]; ok {
		return cached
	}

	rowStyle := a.t.Dialog.NormalItem
	if a.focused {
		rowStyle = a.t.Dialog.SelectedItem
	}

	// Line 1: [TYPE] chip (bold, colored) + station name
	chip := a.t.TagInfo.Bold(true).Render(strings.ToUpper(a.entry.Type))
	station := a.t.Tool.NameNormal.Render(a.entry.Station)
	titleLine := chip + " " + station

	// Line 2: content preview, truncated and muted
	preview := truncate(firstLine(a.entry.Content), maxPreviewChars)
	previewStyle := a.t.Subtle
	if a.focused {
		previewStyle = a.t.Base
	}
	previewLine := previewStyle.Render(ansi.Truncate(preview, max(0, width-2), "…"))

	content := titleLine + "\n" + previewLine
	result := rowStyle.Width(width).Render(content)

	if a.cache == nil {
		a.cache = make(map[int]string)
	}
	a.cache[width] = result
	return result
}

// artifactItems converts a slice of ArtifactEntry to FilterableItems.
func artifactItems(t *styles.Styles, entries []ArtifactEntry) []list.FilterableItem {
	items := make([]list.FilterableItem, len(entries))
	for i, e := range entries {
		items[i] = &ArtifactItem{entry: e, t: t}
	}
	return items
}

// firstLine returns the first non-empty line of text, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// truncate limits s to n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
