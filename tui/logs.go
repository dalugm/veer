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
	for _, source := range []string{"access", "error", "access/error"} {
		if rest, ok := strings.CutPrefix(text, "["+source+"] "); ok {
			entry.source, text = source, rest
			break
		}
	}
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
		if entry.source == "—" {
			entry.source = match[2]
			text = match[1] + text[len(match[0]):]
		}
	}
	entry.message = text
	return entry
}

func (m *Model) logLines(w int) []string {
	logs := m.snapshot.Logs
	if m.archivedLogs() {
		logs = m.logSearch.result.Lines
	}
	var rows []string
	for _, raw := range logs {
		rows = append(rows, m.logRows(raw, w)...)
	}
	return rows
}

// logRows renders one raw log line as zero or more wrapped view rows.
func (m *Model) logRows(raw string, w int) []string {
	if !m.logMatches(raw) {
		return nil
	}
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
	var rows []string
	for i, part := range strings.Split(ansi.Hardwrap(entry.message, max(1, w-logPrefixWidth), false), "\n") {
		lead := strings.Repeat(" ", logPrefixWidth)
		if i == 0 {
			lead = shade.Render(prefix)
		}
		rows = append(rows, lead+base.Render(part))
	}
	return rows
}

func (m *Model) logBody(w, h int) string {
	rows := m.logLines(w)
	end := max(0, len(rows)-m.logOffset)
	start := max(0, end-max(0, h-1))
	header := faint.Render("TIME     LEVEL SOURCE           MESSAGE")
	if m.snapshot.LogError != "" {
		header += "\n" + faint.Render(safe(m.snapshot.LogError))
	}
	if len(rows) == 0 {
		if m.logFilter != "" {
			return header + "\n\n" + faint.Render("No matching logs. Press / to edit the search.")
		}
		return header + "\n\n" + faint.Render("No engine logs yet. Connect a profile.")
	}
	return header + "\n" + strings.Join(rows[start:end], "\n")
}

func (m *Model) logsView(w, h int) string {
	mode := "LIVE"
	if m.logOffset > 0 || (m.archivedLogs() && m.logSearch.result.Skip > 0) {
		mode = "SCROLLED · G follow"
	}
	if m.logFilter != "" {
		matches := 0
		for _, line := range m.snapshot.Logs {
			if m.logMatches(line) {
				matches++
			}
		}
		total := len(m.snapshot.Logs)
		if m.archivedLogs() {
			matches = m.logSearch.result.Matches
			total = int(m.logSearch.count)
		}
		mode += fmt.Sprintf(
			" · / %s · %d/%d",
			clip(safe(m.logFilter), 24),
			matches,
			total,
		)
		if m.archivedLogs() && matches > 0 {
			end := matches - m.logSearch.result.Skip
			mode += fmt.Sprintf(" · %d–%d", max(1, end-len(m.logSearch.result.Lines)+1), end)
		}
		if m.logSearch.loading {
			mode += " · Searching…"
		}
	}
	return box("ENGINE LOGS  /  "+mode, m.logBody(w-4, h-4), w, h)
}

func (m *Model) logMatches(line string) bool {
	return strings.Contains(
		strings.ToLower(safe(line)),
		strings.ToLower(strings.TrimSpace(m.logFilter)),
	)
}

// logPinState remembers the tail of the log buffer already accounted for by
// the current scroll offset, so later refreshes can measure what was appended.
type logPinState struct {
	count uint64
	width int
}

// refreshSnapshot swaps in the latest session snapshot. A log view scrolled
// away from the tail stays put: its offset grows by the rows appended since
// the previous refresh, so streaming output no longer pushes the lines being
// read off screen. At the bottom (offset zero) the view keeps following it.
func (m *Model) refreshSnapshot() {
	pin := m.logPin
	m.snapshot = m.backend.Snapshot()
	m.logPin.count, m.logPin.width = m.snapshot.LogCount, m.width
	if m.logOffset <= 0 || m.archivedLogs() {
		return
	}
	if m.width == pin.width && m.snapshot.LogCount >= pin.count {
		first := int64(m.snapshot.LogCount) - int64(len(m.snapshot.Logs))
		start := max(0, int(int64(pin.count)-first))
		m.logOffset += m.countLogRows(m.snapshot.Logs[start:], m.logWidth())
	}
	// Once the pinned entries have left the live buffer, show the oldest
	// remaining page instead of scrolling beyond all retained rows.
	m.logOffset = min(m.logOffset, max(0, len(m.logLines(m.logWidth()))-m.navigationRows()))
}

// countLogRows returns the number of view rows the raw lines occupy.
func (m *Model) countLogRows(lines []string, w int) int {
	rows := 0
	for _, raw := range lines {
		rows += len(m.logRows(raw, w))
	}
	return rows
}
