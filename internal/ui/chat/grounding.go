package chat

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

// GroundingMessageItem renders Google Search grounding metadata (queries + sources).
type GroundingMessageItem struct {
	*cachedMessageItem

	id        string
	grounding message.GroundingContent
	sty       *styles.Styles
}

var _ MessageItem = (*GroundingMessageItem)(nil)

// NewGroundingMessageItem creates a new grounding item.
func NewGroundingMessageItem(sty *styles.Styles, messageID string, gc *message.GroundingContent) MessageItem {
	return &GroundingMessageItem{
		cachedMessageItem: &cachedMessageItem{},
		id:                fmt.Sprintf("%s:grounding", messageID),
		grounding:         *gc,
		sty:               sty,
	}
}

// ID implements MessageItem.
func (g *GroundingMessageItem) ID() string { return g.id }

// RawRender implements MessageItem.
func (g *GroundingMessageItem) RawRender(width int) string {
	innerWidth := max(0, width-MessageLeftPaddingTotal)
	content, _, ok := g.getCachedRender(innerWidth)
	if !ok {
		content = g.renderContent(innerWidth)
		g.setCachedRender(content, innerWidth, lipgloss.Height(content))
	}
	return content
}

// Render implements MessageItem.
func (g *GroundingMessageItem) Render(width int) string {
	prefix := g.sty.Chat.Message.SectionHeader.Render()
	lines := strings.Split(g.RawRender(width), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

const groundingExpandThreshold = 5

// indentWrap hard-wraps styled text to fit within width, indenting every line
// by indent spaces. Content should not include leading indentation.
func indentWrap(content string, width, indent int) string {
	avail := max(1, width-indent)
	wrapped := ansi.Hardwrap(content, avail, true)
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (g *GroundingMessageItem) renderContent(width int) string {
	icon := g.sty.Tool.IconSuccess.Render(styles.ToolSuccess)
	name := g.sty.Tool.NameNormal.Render("Google Search")

	var queryStr string
	if len(g.grounding.Queries) > 0 {
		queryStr = g.sty.Tool.ParamMain.Render(strings.Join(g.grounding.Queries, ", "))
	}

	header := fmt.Sprintf("%s %s %s", icon, name, queryStr)
	header = ansi.Truncate(header, max(0, width), "…")

	if len(g.grounding.Sources) == 0 {
		return common.Section(g.sty, header, width)
	}

	if len(g.grounding.Sources) <= groundingExpandThreshold {
		return common.Section(g.sty, header+"\n"+g.renderExpanded(width), width)
	}
	return common.Section(g.sty, header+"\n"+g.renderCompact(width), width)
}

func (g *GroundingMessageItem) renderCompact(width int) string {
	var domains []string
	seen := make(map[string]bool)
	for _, src := range g.grounding.Sources {
		d := src.Domain
		if d == "" {
			d = src.Title
		}
		if !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	sourceLine := g.sty.Chat.Message.AssistantInfoModel.Render(
		fmt.Sprintf("%d sources: %s", len(g.grounding.Sources), strings.Join(domains, " · ")),
	)
	return ansi.Hardwrap(sourceLine, max(1, width), true)
}

func (g *GroundingMessageItem) renderExpanded(width int) string {
	lines := make([]string, 0, len(g.grounding.Sources)*2)

	for i, src := range g.grounding.Sources {
		domain := src.Domain
		if domain == "" {
			domain = src.Title
		}
		idx := g.sty.Chat.Message.AssistantInfoModel.Render(fmt.Sprintf("[%d]", i+1))
		title := g.sty.Tool.ParamMain.Render(src.Title)
		domainStr := g.sty.Chat.Message.AssistantInfoModel.Render(domain)
		titleContent := fmt.Sprintf("%s %s  %s", idx, title, domainStr)
		lines = append(lines, indentWrap(titleContent, width, 2))
		urlContent := g.sty.Chat.Message.AssistantInfoModel.Render(src.URL)
		lines = append(lines, indentWrap(urlContent, width, 6))
	}

	lines = append(lines, g.renderSupports(width)...)
	lines = append(lines, g.renderCitations(width)...)

	return strings.Join(lines, "\n")
}

func (g *GroundingMessageItem) renderSupports(width int) []string {
	if len(g.grounding.Supports) == 0 {
		return nil
	}
	lines := make([]string, 0, len(g.grounding.Supports)+1)
	lines = append(lines, g.sty.Chat.Message.AssistantInfoModel.Render("  Grounded claims:"))
	for _, s := range g.grounding.Supports {
		text := s.Text
		if text == "" {
			text = "(segment)"
		}
		var refs []string
		for _, idx := range s.ChunkIndices {
			refs = append(refs, fmt.Sprintf("[%d]", idx+1))
		}
		claimContent := g.sty.Chat.Message.AssistantInfoModel.Render(
			fmt.Sprintf("\"%s\" → %s", text, strings.Join(refs, ", ")),
		)
		lines = append(lines, indentWrap(claimContent, width, 4))
	}
	return lines
}

func (g *GroundingMessageItem) renderCitations(width int) []string {
	if len(g.grounding.Citations) == 0 {
		return nil
	}
	lines := make([]string, 0, len(g.grounding.Citations)+1)
	lines = append(lines, g.sty.Chat.Message.AssistantInfoModel.Render("  Citations:"))
	for i, c := range g.grounding.Citations {
		license := ""
		if c.License != "" {
			license = fmt.Sprintf(" (%s)", c.License)
		}
		citeContent := g.sty.Chat.Message.AssistantInfoModel.Render(
			fmt.Sprintf("[%d] \"%s\" — %s%s", i+1, c.Title, c.URI, license),
		)
		lines = append(lines, indentWrap(citeContent, width, 4))
	}
	return lines
}
