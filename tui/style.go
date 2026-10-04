package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	ink    = lipgloss.Color("#DCE4F2")
	muted  = lipgloss.Color("#8493AD")
	cyan   = lipgloss.Color("#56D8D0")
	purple = lipgloss.Color("#AE9BFF")
	rose   = lipgloss.Color("#F28DA5")
	border = lipgloss.Color("#35415B")
	base   = lipgloss.NewStyle().Foreground(ink)
	faint  = lipgloss.NewStyle().Foreground(muted)
	accent = lipgloss.NewStyle().Foreground(cyan)
	strong = lipgloss.NewStyle().Bold(true).Foreground(ink)
)

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
func clip(s string, w int) string { return ansi.Truncate(s, max(0, w), "…") }
func box(title, body string, width, height int) string {
	inner := max(1, width-4)
	content := strong.Render(clip(title, inner)) + "\n\n" + body
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = clip(lines[i], inner)
	}
	if height > 0 && len(lines) > height-2 {
		lines = lines[:max(1, height-2)]
	}
	style := base.Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(1, width))
	if height > 0 {
		style = style.Height(max(1, height))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = clip(lines[i], w)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
