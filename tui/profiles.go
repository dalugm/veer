package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) visibleProfiles() []int {
	query := strings.ToLower(strings.TrimSpace(m.profileFilter))
	indices := make([]int, 0, len(m.config.Profiles))
	for i, p := range m.config.Profiles {
		if query == "" ||
			strings.Contains(strings.ToLower(safe(p.Name+" "+p.Engine+" "+p.Path)), query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *Model) ensureProfileCursor() {
	indices := m.visibleProfiles()
	if len(indices) == 0 {
		m.cursor = -1
		return
	}
	if !slices.Contains(indices, m.cursor) {
		m.cursor = indices[0]
	}
}

func (m *Model) hasFocusedProfile() bool {
	return m.cursor >= 0 && m.cursor < len(m.config.Profiles) &&
		slices.Contains(m.visibleProfiles(), m.cursor)
}

func (m *Model) profileBody(w, h int) string {
	indices := m.visibleProfiles()
	search := "/ Search profiles"
	if m.profileFilter != "" {
		search = "/ " + safe(m.profileFilter) + "  · Esc clear"
	}
	lines := []string{faint.Render(clip(search, w))}
	if len(indices) == 0 {
		text := "No matching profiles. Press / to edit the search."
		if len(m.config.Profiles) == 0 {
			text = "No profiles yet. Press a to add a config."
		}
		return strings.Join(append(lines, "", clip(text, w)), "\n")
	}
	nameWidth := min(24, max(8, w/3))
	engineWidth := 8
	pathWidth := max(1, w-nameWidth-engineWidth-4)
	nameColumn := lipgloss.NewStyle().Width(nameWidth)
	engineColumn := lipgloss.NewStyle().Width(engineWidth)
	columns := func(name, engine, path string) string {
		path = safe(path)
		if overflow := ansi.StringWidth(path) - pathWidth; overflow > 0 {
			path = ansi.TruncateLeft(path, overflow+1, "…")
		}
		return nameColumn.Render(clip(safe(name), nameWidth)) + "  " +
			engineColumn.Render(clip(safe(engine), engineWidth)) + "  " +
			path
	}
	lines = append(lines, faint.Render(columns("  NAME", "ENGINE", "PATH")))
	rows := max(1, h-5)
	position := max(0, slices.Index(indices, m.cursor))
	start := max(0, position-rows+1)
	for _, i := range indices[start:min(len(indices), start+rows)] {
		p := m.config.Profiles[i]
		mark := "○"
		if p.ID == m.config.Selected {
			mark = "●"
		}
		label := columns(mark+" "+p.Name, p.Engine, p.Path)
		if i == m.cursor {
			label = lipgloss.NewStyle().
				Background(lipgloss.Color("#253550")).
				Foreground(cyan).
				Bold(true).
				Width(w).
				Render(label)
		}
		lines = append(lines, label)
	}
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	if m.hasFocusedProfile() {
		p := m.config.Profiles[m.cursor]
		state := "Enter to select"
		if p.ID == m.config.Selected {
			state = "Selected"
		}
		lines = append(lines, faint.Render(strings.Repeat("─", w)),
			spread(strong.Render(clip(safe(p.Name), max(8, w-14))), faint.Render(state), w),
			faint.Render(clip(safe(p.Path), w)))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) profilesView(w, h int) string {
	title := fmt.Sprintf("PROFILES  /  %d of %d", len(m.visibleProfiles()), len(m.config.Profiles))
	return box(title, m.profileBody(w-4, h-4), w, h)
}
