package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/session"
)

func (m *Model) overviewView(w, h int) string {
	name := "Add a profile [a]"
	p, selected := m.config.Active()
	if selected {
		name = safe(p.Name)
	} else if len(m.config.Profiles) > 0 {
		name = "Choose a profile [2]"
	}
	title := spread(
		m.connectionStatus()+faint.Render(" · ")+strong.Render(name),
		faint.Render(m.coreLabel()),
		w,
	)
	mode, endpoint := "No profile selected", ""
	if selected {
		mode = m.profileMode()
		endpoint = "No listener configured"
		if len(m.info.Endpoints) > 0 {
			label := "Configured listeners  "
			if m.snapshot.State == session.Running {
				label = "LISTEN  "
			}
			endpoint = label + safe(strings.Join(m.info.Endpoints, " · "))
		}
	}
	if m.snapshot.Error != "" {
		endpoint = safe(m.snapshot.Error)
	}
	summary := faint.Render(clip(mode, w)) + "\n" + faint.Render(clip(endpoint, w))
	gap := ""
	reserve := 3
	if h >= 14 {
		gap = "\n"
		reserve = 5
	}
	return title + "\n" + gap + m.trafficView(w, h-reserve) + "\n" + gap + summary
}

func (m *Model) profileMode() string {
	mode := "Local proxy · DNS System"
	if m.info.TUN {
		mode = "TUN · DNS " + m.dnsDescription()
	}
	if m.snapshot.State != session.Running {
		mode = "Selected profile · " + mode
	}
	return mode
}

func (m *Model) detailLines(w int) []string {
	lines := []string{}
	p, ok := m.config.Active()
	if m.page == Profiles {
		ok = m.hasFocusedProfile()
		if ok {
			p = m.config.Profiles[m.cursor]
		}
	}
	if ok {
		lines = append(lines, "PROFILE  "+safe(p.Name))
	}
	if m.page == Overview {
		lines = append(lines, "CORE     "+m.coreLabel())
		if m.snapshot.PID > 0 {
			lines = append(
				lines,
				fmt.Sprintf(
					"PROCESS  PID %d · %s",
					m.snapshot.PID,
					time.Since(m.snapshot.Since).Truncate(time.Second),
				),
			)
		}
		if ok {
			lines = append(lines, "MODE     "+m.profileMode())
		}
		if ok && m.info.TUN {
			routes := "Manual"
			switch {
			case m.info.TUNIPv4 && m.info.TUNIPv6:
				routes = "IPv4 + IPv6"
			case m.info.TUNIPv4:
				routes = "IPv4 only"
			case m.info.TUNIPv6:
				routes = "IPv6 only"
			}
			lines = append(lines, "ROUTES   "+routes)
		}
		if ok {
			for _, endpoint := range m.info.Endpoints {
				label := "LISTEN   "
				if m.snapshot.State != session.Running {
					label = "CONFIGURED LISTEN   "
				}
				lines = append(lines, label+safe(endpoint))
			}
		}
	}
	lines = append(lines, "BINARY   "+safe(m.config.EnginePath))
	if ok {
		lines = append(lines, "CONFIG   "+safe(p.Path))
		if source, found := m.config.Subscription(p.SubscriptionID); found {
			lines = append(
				lines,
				"SOURCE   "+safe(source.Name),
				"PRESET   "+safe(source.ConfigMode()),
			)
			if source.ConfigMode() == "custom" {
				lines = append(lines, "BASE     "+safe(source.Template))
				if source.OutboundTag != "" {
					lines = append(lines, "OUTBOUND "+safe(source.OutboundTag))
				}
			}
		}
	}
	var rows []string
	for _, line := range lines {
		rows = append(rows, strings.Split(ansi.Hardwrap(line, w, false), "\n")...)
	}
	return rows
}
func (m *Model) detailsRows() int  { return max(1, m.height-8) }
func (m *Model) detailsWidth() int { return max(1, min(m.width-2, 100)-4) }
func (m *Model) detailsKey(key string) {
	m.pendingG = false
	limit := max(0, len(m.detailLines(m.detailsWidth()))-m.detailsRows())
	switch key {
	case "esc", "q", "i":
		m.showDetails = false
	case "j", "down":
		m.detailOffset++
	case "k", "up":
		m.detailOffset--
	case "ctrl+d":
		m.detailOffset += max(1, m.detailsRows()/2)
	case "ctrl+u":
		m.detailOffset -= max(1, m.detailsRows()/2)
	case "ctrl+f", "pgdown":
		m.detailOffset += m.detailsRows()
	case "ctrl+b", "pgup":
		m.detailOffset -= m.detailsRows()
	case "G", "end":
		m.detailOffset = limit
	case "g", "home":
		m.detailOffset = 0
	}
	m.detailOffset = min(limit, max(0, m.detailOffset))
}

func (m *Model) detailsView(w, h int) string {
	lines := m.detailLines(m.detailsWidth())
	rows := m.detailsRows()
	start := min(m.detailOffset, max(0, len(lines)-rows))
	body := fit(
		strings.Join(lines[start:min(len(lines), start+rows)], "\n"),
		m.detailsWidth(),
		rows,
	)
	body += "\n\n" + faint.Render("j/k scroll · G end · Esc/q close")
	content := box("CONNECTION DETAILS", body, min(w-2, 100), h-2)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

// Connection status distinguishes listener readiness from merely having a process.
func (m *Model) connectionStatus() string {
	if m.auth != nil {
		return accent.Render("◌ Authorizing")
	}
	switch m.snapshot.State {
	case session.Validating, session.Starting:
		return accent.Render("◌ Connecting")
	case session.Running:
		if m.snapshot.Ready {
			return accent.Render("● Connected")
		}
		return faint.Render("○ Core running")
	case session.Stopping:
		return faint.Render("◌ Disconnecting")
	case session.Failed:
		return lipgloss.NewStyle().Foreground(rose).Render("● Failed")
	default:
		return faint.Render("○ Disconnected")
	}
}
