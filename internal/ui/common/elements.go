package common

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/home"
	"github.com/dmora/crucible/internal/ui/styles"
)

// PrettyPath formats a file path with home directory shortening and applies
// muted styling.
func PrettyPath(t *styles.Styles, path string, width int) string {
	formatted := home.Short(path)
	return t.Muted.Width(width).Render(formatted)
}

// ModelInfo renders model information including name, provider, auth info,
// and reasoning settings.
func ModelInfo(t *styles.Styles, modelName, providerName, reasoningInfo string, authInfo *config.AuthInfo, width int) string {
	modelIcon := t.Subtle.Render(styles.ModelIcon)
	modelName = t.Base.Render(modelName)

	// Build first line with model name and optionally provider on the same line
	var firstLine string
	if providerName != "" {
		providerInfo := t.Muted.Render(fmt.Sprintf("via %s", providerName))
		modelWithProvider := fmt.Sprintf("%s %s %s", modelIcon, modelName, providerInfo)

		// Check if it fits on one line
		if lipgloss.Width(modelWithProvider) <= width {
			firstLine = modelWithProvider
		} else {
			// If it doesn't fit, put provider on next line
			firstLine = fmt.Sprintf("%s %s", modelIcon, modelName)
		}
	} else {
		firstLine = fmt.Sprintf("%s %s", modelIcon, modelName)
	}

	parts := []string{firstLine}

	// If provider didn't fit on first line, add it as second line
	if providerName != "" && !strings.Contains(firstLine, "via") {
		providerInfo := fmt.Sprintf("via %s", providerName)
		parts = append(parts, t.Muted.PaddingLeft(2).Render(providerInfo))
	}

	if authInfo != nil {
		for _, line := range formatAuthInfo(authInfo) {
			parts = append(parts, t.Subtle.PaddingLeft(2).Render(line))
		}
	}

	if reasoningInfo != "" {
		parts = append(parts, t.Subtle.PaddingLeft(2).Render(reasoningInfo))
	}

	return lipgloss.NewStyle().Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left, parts...),
	)
}

// formatAuthInfo returns lines describing the auth method and backend.
func formatAuthInfo(auth *config.AuthInfo) []string {
	switch auth.Backend {
	case config.GeminiBackendVertex:
		methodLine := string(auth.Method)
		if auth.User != "" {
			methodLine += " · " + auth.User
		}
		lines := []string{methodLine}
		if auth.Project != "" {
			lines = append(lines, "Project: "+auth.Project)
		}
		if auth.Location != "" {
			lines = append(lines, "Location: "+auth.Location)
		}
		return lines
	case config.GeminiBackendAPI:
		line := string(auth.Method)
		if auth.User != "" {
			line += " · " + auth.User
		}
		return []string{line}
	default:
		return []string{"Unknown backend"}
	}
}

// FormatTokenCount formats a token count with appropriate units (K/M).
func FormatTokenCount(tokens int64) string {
	var s string
	switch {
	case tokens >= 1_000_000:
		s = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		s = fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		s = fmt.Sprintf("%d", tokens)
	}
	if strings.HasSuffix(s, ".0K") {
		s = strings.Replace(s, ".0K", "K", 1)
	}
	if strings.HasSuffix(s, ".0M") {
		s = strings.Replace(s, ".0M", "M", 1)
	}
	return s
}

// StatusOpts defines options for rendering a status line with icon, title,
// description, and optional extra content.
type StatusOpts struct {
	Icon             string // if empty no icon will be shown
	Title            string
	TitleColor       color.Color
	Description      string
	DescriptionColor color.Color
	ExtraContent     string // additional content to append after the description
}

// Status renders a status line with icon, title, description, and extra
// content. The description is truncated if it exceeds the available width.
func Status(t *styles.Styles, opts StatusOpts, width int) string {
	icon := opts.Icon
	title := opts.Title
	description := opts.Description

	titleColor := cmp.Or(opts.TitleColor, t.Muted.GetForeground())
	descriptionColor := cmp.Or(opts.DescriptionColor, t.Subtle.GetForeground())

	title = t.Base.Foreground(titleColor).Render(title)

	if description != "" {
		extraContentWidth := lipgloss.Width(opts.ExtraContent)
		if extraContentWidth > 0 {
			extraContentWidth += 1
		}
		description = ansi.Truncate(description, width-lipgloss.Width(icon)-lipgloss.Width(title)-2-extraContentWidth, "…")
		description = t.Base.Foreground(descriptionColor).Render(description)
	}

	var content []string
	if icon != "" {
		content = append(content, icon)
	}
	content = append(content, title)
	if description != "" {
		content = append(content, description)
	}
	if opts.ExtraContent != "" {
		content = append(content, opts.ExtraContent)
	}

	return strings.Join(content, " ")
}

// Section renders a section header with a title and a horizontal line filling
// the remaining width.
func Section(t *styles.Styles, text string, width int, info ...string) string {
	char := styles.SectionSeparator
	length := lipgloss.Width(text) + 1
	remainingWidth := width - length

	var infoText string
	if len(info) > 0 {
		infoText = strings.Join(info, " ")
		if len(infoText) > 0 {
			infoText = " " + infoText
			remainingWidth -= lipgloss.Width(infoText)
		}
	}

	text = t.Section.Title.Render(text)
	if remainingWidth > 0 {
		text = text + " " + t.Section.Line.Render(strings.Repeat(char, remainingWidth)) + infoText
	}
	return text
}

// DialogTitle renders a dialog title with a decorative line filling the
// remaining width.
func DialogTitle(t *styles.Styles, title string, width int, fromColor, toColor color.Color) string {
	char := "╱"
	length := lipgloss.Width(title) + 1
	remainingWidth := width - length
	if remainingWidth > 0 {
		lines := strings.Repeat(char, remainingWidth)
		lines = styles.ApplyForegroundGrad(t, lines, fromColor, toColor)
		title = title + " " + lines
	}
	return title
}
