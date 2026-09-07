package tui

import "slices"

// navigate handles normal-mode navigation only. Forms and confirmations receive
// their keys before this function, so text input never becomes a Vim command.
func (m *Model) navigate(key string) bool {
	if key == "g" && (m.page == Profiles || m.page == Logs) {
		if m.pendingG {
			m.jumpNavigation(false)
		}
		m.pendingG = !m.pendingG
		return true
	}
	m.pendingG = false
	rows := m.navigationRows()
	switch key {
	case "h", "left", "shift+tab":
		m.page = (m.page + Page(len(pageNames)) - 1) % Page(len(pageNames))
	case "l", "right", "tab":
		m.page = (m.page + 1) % Page(len(pageNames))
	case "j", "down":
		m.moveNavigation(1)
	case "k", "up":
		m.moveNavigation(-1)
	case "ctrl+d":
		m.moveNavigation(max(1, rows/2))
	case "ctrl+u":
		m.moveNavigation(-max(1, rows/2))
	case "ctrl+f", "pgdown":
		m.moveNavigation(rows)
	case "ctrl+b", "pgup":
		m.moveNavigation(-rows)
	case "G", "end":
		m.jumpNavigation(true)
	case "home":
		m.jumpNavigation(false)
	default:
		return false
	}
	return true
}

func (m *Model) navigationRows() int {
	if m.page == Logs {
		return max(1, m.height-13)
	}
	return max(1, m.height-17)
}

func (m *Model) logWidth() int {
	return m.width - 6
}

func (m *Model) moveNavigation(delta int) {
	switch m.page {
	case Profiles:
		indices := m.visibleProfiles()
		if len(indices) == 0 {
			m.cursor = -1
			return
		}
		pos := max(0, slices.Index(indices, m.cursor))
		m.cursor = indices[min(len(indices)-1, max(0, pos+delta))]
	case Logs:
		m.logOffset = min(
			max(0, len(m.logLines(m.logWidth()))-m.navigationRows()),
			max(0, m.logOffset-delta),
		)
	}
}

func (m *Model) jumpNavigation(last bool) {
	switch m.page {
	case Profiles:
		indices := m.visibleProfiles()
		if len(indices) == 0 {
			m.cursor = -1
			return
		}
		m.cursor = indices[0]
		if last {
			m.cursor = indices[len(indices)-1]
		}
	case Logs:
		m.logOffset = 0
		if !last {
			m.logOffset = max(0, len(m.logLines(m.logWidth()))-m.navigationRows())
		}
	}
}
