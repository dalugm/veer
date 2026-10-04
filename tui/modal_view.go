package tui

import (
	"fmt"
	"strings"
)

func (m *Model) formView(w, h int) string {
	f := m.form
	if f.pathField() {
		return m.pathCompletionView(w, h)
	}
	// Scroll the form by whole fields so the focused input remains visible.
	count := max(1, (h-7)/3)
	start := max(0, f.focus-count+1)
	end := min(len(f.inputs), start+count)
	description := f.description
	if start > 0 || end < len(f.inputs) {
		description = fmt.Sprintf(
			"Fields %d–%d of %d · Tab to navigate",
			start+1,
			end,
			len(f.inputs),
		)
	}
	lines := []string{faint.Render(clip(description, w-6)), ""}
	for i := start; i < end; i++ {
		style := faint
		if i == f.focus {
			style = accent
		}
		input := f.inputs[i].View()
		if f.kind == "geo" && i == 1 {
			input = f.geoSourceView()
		}
		lines = append(lines, style.Render(f.labels[i]), input, "")
	}
	lines = append(lines, accent.Render("[ Ctrl+S ] Submit"))
	return box(f.title, strings.Join(lines, "\n"), w, h)
}

func (m *Model) confirmView(w, h int) string {
	title, body := "Confirm", ""
	accept, decline := "[ y / Enter ] Confirm", "[ n / Esc ] Cancel"
	switch m.confirmation {
	case "quit":
		title = "Disconnect and quit?"
		body = "The active Xray session will stop before Veer exits."
	case "remove":
		title = "Remove this profile?"
		body = "Only its entry in Veer is removed. Your config file is retained."
	case "update":
		title = "Install Xray update?"
		if selected := m.selectedUpdate(); selected != nil {
			body = safe(
				m.updates.current,
			) + " → " + safe(
				selected.Version,
			) + "\n" + safe(m.config.EnginePath) + "\nDownload, verify and replace the configured Xray core."
			if m.running() {
				body += "\nAfter installation, choose whether to restart the connection."
			}
		}
	case "restore-update":
		title = "Restore previous Xray?"
		body = safe(
			m.availableBackup(),
		) + "\nRestore the local backup without downloading.\nKeep the current executable as another backup."
		if m.running() {
			body += "\nAfter restoration, choose whether to restart the connection."
		}
	case "restart-update":
		title = "Restart connection now?"
		body = "Xray " + safe(
			m.updates.current,
		) + " is installed.\nRestart briefly disconnects to apply the new core.\nChoose Later to keep the current core running."
		accept, decline = "[ y / Enter ] Restart", "[ n / Esc ] Later"

	}
	return box(
		title,
		body+"\n\n"+accent.Render(
			accept,
		)+"    "+faint.Render(
			decline,
		),
		w,
		h,
	)
}
