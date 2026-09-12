package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m *Model) helpView(w, h int) string {
	lines := []string{
		"h/l or 1–5   Switch pages",
		"Tab/Shift+Tab  Switch to next/previous page",
		"j/k          Move row or scroll current page",
		"gg / G       First / last row; G follows logs",
		"Ctrl+d/u     Half page down/up",
		"Ctrl+f/b     Full page down/up",
		"",
	}
	switch m.page {
	case Profiles:
		lines = append(lines, "/ search · Enter select · Esc clear search",
			"a add · e rename · d remove · y QR · i details",
			"c connect selected profile · s stop")
	case Logs:
		lines = append(lines, "c connect · s stop · a add profile",
			"Log messages wrap inside the message column.",
			"G resumes following the latest output.")
	case Tools:
		lines = append(lines, "g update Geo assets · u update Xray",
			"c connect · s stop · a add profile",
			"v check core version")
	case Settings:
		lines = append(lines, "e edit settings · v check core version",
			"c connect · s stop · a add profile",
			"Forms: Tab next · Ctrl+S save · Esc cancel")
	default:
		lines = append(lines, "c connect · r restart · s stop · a add profile",
			"y share active profile as QR · v core version",
			"i connection details · Tab next page")
	}
	lines = append(lines, "", "Esc / q / ? close help · Ctrl+C stop and quit")
	content := box(
		"KEYBOARD · "+pageNames[m.page],
		strings.Join(lines, "\n"),
		min(w-2, 76),
		16,
	)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}
