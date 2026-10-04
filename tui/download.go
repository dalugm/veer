package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/download"
)

// Download workers update their own state; the TUI samples it on its normal
// refresh tick. A completed or cancelled worker cannot update a later download.
type downloadState struct {
	mu    sync.Mutex
	label string
	files map[string]download.Progress
}

func (m *Model) startDownload(label string) func(download.Progress) {
	state := &downloadState{label: label, files: make(map[string]download.Progress)}
	m.downloads = state
	return func(progress download.Progress) {
		state.mu.Lock()
		state.files[progress.Name] = progress
		state.mu.Unlock()
	}
}

func (d *downloadState) snapshot() (received, total int64, known, done bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	known, done = len(d.files) > 0, len(d.files) > 0
	for _, file := range d.files {
		received += file.Received
		switch {
		case file.Total > 0:
			total += file.Total
		case file.Done:
			total += file.Received
		default:
			known = false
		}
		done = done && file.Done
	}
	return received, total, known && total > 0, done
}

func (m *Model) downloadView(width int) string {
	received, total, known, done := m.downloads.snapshot()
	label := m.downloads.label
	status := byteSize(received)
	if known {
		percent := min(100, received*100/total)
		status = fmt.Sprintf("%3d%% %s/%s", percent, byteSize(received), byteSize(total))
	}
	if m.notice == "Cancelling…" {
		status = "Cancelling… " + status
	} else if done {
		status = "Verifying… " + status
	}
	barWidth := max(1, min(32, width-ansi.StringWidth(label)-ansi.StringWidth(status)-5))
	filled := 0
	if known {
		filled = min(barWidth, int(received*int64(barWidth)/total))
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	if !known {
		cells := []rune(strings.Repeat("░", barWidth))
		for i := range min(3, barWidth) {
			cells[(m.phase+i)%barWidth] = '█'
		}
		bar = string(cells)
	}
	return clip(label+" ["+accent.Render(bar)+"] "+faint.Render(status), width)
}

func byteSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
}
