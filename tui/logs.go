package tui

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type logEntry struct{ time, level, source, message string }

var (
	logTime = regexp.MustCompile(
		`^(?:\d{4}[/-]\d{2}[/-]\d{2}[ T])?(\d{2}:\d{2}:\d{2})(?:[.,]\d+)?(?:Z|[+-]\d{2}:\d{2})?\s+`,
	)
	logLevel  = regexp.MustCompile(`(?i)^\[(debug|info|warning|warn|error|fatal|panic|trace)\]\s*`)
	logSource = regexp.MustCompile(`^(\[\d+\]\s*)?([A-Za-z][A-Za-z0-9_./-]*):\s*`)
)

const logPrefixWidth = 32

func parseLog(raw string) logEntry {
	entry := logEntry{time: "—", level: "—", source: "—"}
	text := safe(raw)
	if match := logTime.FindStringSubmatch(text); match != nil {
		entry.time = match[1]
		text = text[len(match[0]):]
	}
	if match := logLevel.FindStringSubmatch(text); match != nil {
		entry.level = strings.ToUpper(match[1])
		if entry.level == "WARNING" {
			entry.level = "WARN"
		}
		text = text[len(match[0]):]
	}
	if match := logSource.FindStringSubmatch(text); match != nil {
		entry.source = match[2]
		text = match[1] + text[len(match[0]):]
	}
	entry.message = text
	return entry
}

func (m *Model) logLines(w int) []string {
	var rows []string
	for _, raw := range m.snapshot.Logs {
		entry := parseLog(raw)
		shade := faint
		switch entry.level {
		case "ERROR", "FATAL", "PANIC":
			shade = lipgloss.NewStyle().Foreground(rose)
		case "WARN":
			shade = lipgloss.NewStyle().Foreground(lipgloss.Color("#E9C46A"))
		case "INFO":
			shade = accent
		}
		prefix := fmt.Sprintf("%-8s %-5s %-16s ", entry.time, entry.level, clip(entry.source, 16))
		parts := strings.Split(ansi.Hardwrap(entry.message, max(1, w-logPrefixWidth), false), "\n")
		for i, part := range parts {
			lead := strings.Repeat(" ", logPrefixWidth)
			if i == 0 {
				lead = shade.Render(prefix)
			}
			rows = append(rows, lead+base.Render(part))
		}
	}
	return rows
}

func (m *Model) logBody(w, h int) string {
	rows := m.logLines(w)
	end := max(0, len(rows)-m.logOffset)
	start := max(0, end-max(0, h-1))
	header := faint.Render("TIME     LEVEL SOURCE           MESSAGE")
	if len(rows) == 0 {
		return header + "\n\n" + faint.Render("No engine logs yet. Connect a profile.")
	}
	return header + "\n" + strings.Join(rows[start:end], "\n")
}

func (m *Model) logsView(w, h int) string {
	mode := "LIVE"
	if m.logOffset > 0 {
		mode = "SCROLLED · G follow"
	}
	return box("ENGINE LOGS  /  "+mode, m.logBody(w-4, h-4), w, h)
}
