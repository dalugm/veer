package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *Model) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	if m.auth != nil {
		return m.updateAuthorization(msg)
	}
	if m.showDetails {
		m.detailsKey(msg.String())
		return nil
	}
	if m.showHelp {
		switch msg.String() {
		case "esc", "q", "?":
			m.showHelp = false
		}
		return nil
	}
	if m.search != nil {
		return m.updateSearch(msg)
	}
	if m.qr != nil {
		m.qrKey(msg.String())
		return nil
	}
	if m.confirmation != "" {
		m.pendingG = false
		switch msg.String() {
		case "y", "enter":
			choice := m.confirmation
			m.confirmation = ""
			switch choice {
			case "quit":
				return m.quit()
			case "remove":
				return m.removeProfile()
			case "update":
				return m.installUpdate()
			case "restore-update":
				return m.restoreUpdate()
			case "restart-update":
				return m.restart()
			}
		case "n", "esc":
			m.confirmation = ""
		}
		return nil
	}
	if m.form != nil {
		m.pendingG = false
		return m.updateForm(msg)
	}
	if m.updates.open {
		m.pendingG = false
		return m.updateKey(msg.String())
	}
	if m.busy {
		m.pendingG = false
		if msg.String() == "esc" && m.cancelWork != nil {
			m.cancelWork()
			m.notice = "Cancelling…"
		}
		return nil
	}
	return m.pageKey(msg.String())
}

func (m *Model) pageKey(key string) tea.Cmd {
	if handled, cmd := m.navigateArchivedLogs(key); handled {
		return cmd
	}
	previousPage := m.page
	if m.navigate(key) {
		if m.page == Tools && previousPage != Tools {
			return m.loadGeoInfo(m.geoDirectory())
		}
		return nil
	}
	switch key {
	case "i":
		if m.page == Overview || m.page == Profiles {
			m.showDetails = true
			m.detailOffset = 0
		}
	case "/":
		return m.startSearch()
	case "esc":
		switch m.page {
		case Profiles:
			m.profileFilter = ""
			m.ensureProfileCursor()
		case Logs:
			m.logFilter, m.logOffset = "", 0
			m.clearArchiveSearch()
		}
	case "y":
		return m.openQR()
	case "q":
		if m.running() {
			m.confirmation = "quit"
			return nil
		}
		return m.quit()
	case "1", "2", "3", "4", "5":
		m.page = Page(key[0] - '1')
		if m.page == Tools {
			return m.loadGeoInfo(m.geoDirectory())
		}
	case "a":
		m.openImport()
	case "e":
		switch m.page {
		case Settings:
			return m.openSettings()
		case Profiles:
			if m.running() {
				m.bad = true
				m.notice = "Disconnect before editing a profile."
			} else {
				return m.openEdit()
			}
		}
	case "c":
		return m.connect()
	case "s":
		return m.stop(false)
	case "r":
		if m.page == Overview {
			return m.restart()
		}
	case "v":
		return m.checkVersion()
	case "g":
		if m.page == Tools {
			return m.openGeo()
		}
	case "u":
		if m.page == Tools {
			m.updates.open = true
			m.notice, m.bad = "", false
		}
	case "enter":
		if m.page == Profiles {
			return m.selectProfile()
		}
		if m.page == Settings {
			return m.openSettings()
		}
		if m.page == Overview {
			return m.connect()
		}
	case "d", "delete":
		if m.page == Profiles && m.hasFocusedProfile() {
			if m.running() {
				m.bad = true
				m.notice = "Disconnect before removing a profile."
			} else {
				m.confirmation = "remove"
			}
		}
	case "?":
		m.showHelp = true
	}
	return nil
}
