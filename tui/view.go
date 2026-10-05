package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// View renders the current terminal screen.
func (m *Model) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	if w < 60 || h < 18 {
		return m.screen(
			fit(
				" VEER\n\n Please enlarge the terminal.\n Minimum: 60 columns × 18 rows.\n\n Ctrl+C to quit.",
				w,
				h,
			),
		)
	}
	if m.showDetails {
		return m.screen(fit(m.detailsView(w, h), w, h))
	}
	if m.showHelp {
		return m.screen(fit(m.helpView(w, h), w, h))
	}
	header := accent.Bold(true).Render("  V E E R") + faint.Render("  /  where to next?")
	if m.updates.latest != nil {
		header = accent.Bold(true).
			Render("  V E E R") +
			faint.Render(
				"  /  Xray update · 4 then u",
			)
	}
	if m.page != Overview {
		status := m.connectionStatus()
		gap := max(1, w-ansi.StringWidth(header)-ansi.StringWidth(status)-3)
		header += strings.Repeat(" ", gap) + status
	}
	tabs := make([]string, 5)
	for i, name := range pageNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if Page(i) == m.page {
			tabs[i] = lipgloss.NewStyle().
				Background(cyan).
				Foreground(lipgloss.Color("#111827")).
				Bold(true).
				Render(label)
		} else {
			tabs[i] = faint.Render(label)
		}
	}
	nav := " " + strings.Join(tabs, " ")
	contentHeight := h - 8
	var content string
	if m.confirmation != "" {
		content = m.confirmView(w-2, contentHeight)
	} else if m.form != nil {
		content = m.formView(w-2, contentHeight)
	} else if m.updates.open {
		content = m.updateView(w-2, contentHeight)
	} else {
		switch m.page {
		case Overview:
			content = m.overviewView(w-2, contentHeight)
		case Profiles:
			content = m.profilesView(w-2, contentHeight)
		case Logs:
			content = m.logsView(w-2, contentHeight)
		case Tools:
			content = m.toolsView(w-2, contentHeight)
		case Settings:
			content = m.settingsView(w-2, contentHeight)
		}
	}
	noticeStyle := faint
	if m.bad {
		noticeStyle = lipgloss.NewStyle().Foreground(rose)
	}
	notice := " " + noticeStyle.Render(clip(safe(m.notice), w-2))
	if m.busy && m.downloads != nil {
		notice = " " + m.downloadView(w-2)
	}
	footer := " c connect  r restart  s stop  y QR  i details  ? help  q quit"
	if m.form != nil {
		footer = " Tab next field   Ctrl+S submit   Esc cancel"
		if m.form.pathField() {
			footer = " ↑/↓ choose  Tab complete  Enter next  Esc cancel"
		}
		if m.form.geoSourceField() {
			footer = " h/l or ←/→ source   Enter download   Esc cancel"
		}
	} else if m.page == Profiles {
		footer = " a add  e edit  d delete  c connect  j/k move  Enter select  / search  y QR  q quit"
	} else if m.page == Logs {
		footer = " j/k scroll  gg/G ends  / search  Esc clear  Ctrl+d/u half page  ? help  q quit"
	} else if m.page == Tools {
		footer = " g Geo assets   u Xray update   ? help   q quit"
	} else if m.page == Settings {
		footer = " e edit   v Xray version"
	}
	if m.search != nil {
		notice = " " + m.search.input.View()
		footer = " Enter apply search   Ctrl+U clear   Esc cancel"
	}
	if m.updates.open {
		footer = " j/k version  h/l channel  r check  Enter install  Esc back"
	}
	if m.busy {
		footer = " Esc cancel current operation   Ctrl+C stop and quit"
	}
	// On narrow terminals, keep the footer to a single discoverable action.
	// Context-specific shortcuts remain available from the help view.
	if w < 82 {
		footer = " ? help"
	}
	return m.screen(
		fit(
			header+"\n\n"+nav+"\n\n"+content+"\n"+notice+"\n"+faint.Render(clip(footer, w))+"\n",
			w,
			h,
		),
	)
}

func (m *Model) screen(content string) tea.View {
	modal := ""
	if m.auth != nil {
		modal = m.authorizationView(max(1, m.width), max(1, m.height))
	} else if m.qr != nil {
		modal = m.qrView(max(1, m.width), max(1, m.height))
	}
	if modal != "" {
		background := faint.Render(ansi.Strip(content))
		content = lipgloss.NewCompositor(
			lipgloss.NewLayer(background),
			lipgloss.NewLayer(modal).
				X(max(0, (m.width-lipgloss.Width(modal))/2)).
				Y(max(0, (m.height-lipgloss.Height(modal))/2)).
				Z(1),
		).Render()
		content = fit(content, max(1, m.width), max(1, m.height))
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Veer · where to next?"
	v.BackgroundColor = lipgloss.Color("#111827")
	v.ForegroundColor = ink
	return v
}
