package tui

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	update "github.com/dalugm/veer/engine/coreupdate"
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
	footer := " c connect  r restart  s stop  y QR  i details  ? help  q quit"
	if w < 64 {
		footer = " c connect  r restart  s stop  y QR  ? help  q quit"
	}
	if m.form != nil {
		footer = " Tab next field   Ctrl+S submit   Esc cancel"
		if m.form.pathField() {
			footer = " ↑/↓ choose  Tab complete  Enter next  Esc cancel"
		}
		if m.form.geoSourceField() {
			footer = " h/l or ←/→ source   Enter download   Esc cancel"
		}
	} else if m.page == Profiles {
		footer = " a add  e rename  d delete  c connect  j/k move  Enter select  / search  y QR  q quit"
		if w < 82 {
			footer = " a add  e edit  d del  c connect  j/k  Enter  / search  y QR  q quit"
		}
	} else if m.page == Logs {
		footer = " j/k scroll  gg/G ends  Ctrl+d/u half page  ? help  q quit"
	} else if m.page == Tools {
		footer = " g Geo assets   u Xray update   h/l pages   ? help   q quit"
	} else if m.page == Settings {
		footer = " e edit settings   v check engine version   h/l pages"
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

func (m *Model) coreLabel() string {
	version := m.version
	if m.running() {
		version = m.runningVersion
		if version == "" {
			version = "Unavailable"
		}
	}
	if parsed := update.ParseVersion(version); parsed != "" {
		return "Xray " + strings.TrimPrefix(parsed, "v")
	}
	fields := strings.Fields(safe(version))
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Xray") {
		return "Xray " + fields[1]
	}
	return "Xray · " + safe(version)
}

func (m *Model) dnsDescription() string {
	switch m.config.DNSMode {
	case "off":
		return "Off (system unchanged)"
	case "custom":
		return "Custom · " + safe(m.config.DNS)
	default:
		if runtime.GOOS == "windows" {
			return "Automatic · core-managed (system unchanged)"
		}
		return "Automatic · 1.1.1.1, 8.8.8.8"
	}
}

func (m *Model) toolsView(w, h int) string {
	return box(
		"TOOLS",
		accent.Bold(true).
			Render("[ g ]  Geo assets    [ u ]  Xray update")+
			"\n\n"+m.geoAssetsView(h < 14)+"\n\n"+faint.Render("Downloads happen only when you request them."),
		w,
		h,
	)
}

func (m *Model) settingsView(w, h int) string {
	geo := m.config.GeoDir
	if geo == "" {
		geo = "Config directory"
	}
	dns := m.dnsDescription()
	service := m.config.NetworkService
	if service == "" {
		service = "Automatic (active default route)"
	}
	body := faint.Render(
		"XRAY",
	) + "    " + safe(
		m.config.EnginePath,
	) + "\n\n" + faint.Render(
		"VERSION",
	) + " " + safe(
		m.version,
	) + "\n\n" + faint.Render(
		"GEO",
	) + "     " + safe(
		geo,
	) + "\n" + faint.Render(
		"TUN DNS",
	) + " " + safe(
		dns,
	) + "\n" + faint.Render(
		"SERVICE",
	) + " " + safe(
		service,
	) + "\n\n" + faint.Render(
		"SAVED TO",
	) + "\n" + safe(
		m.path,
	)
	return box("SETTINGS", body, w, h)
}

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
