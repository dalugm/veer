package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	update "github.com/dalugm/veer/engine/coreupdate"
	"github.com/dalugm/veer/session"
)

// Update handles terminal input and asynchronous results.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		if result, ok := message.(sessionMsg); ok && result.quit {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg := message.(type) {
	case updateCheckedMsg:
		m.updateChecked(msg)
		return m, nil
	case updateInstalledMsg:
		m.updateInstalled(msg)
		if msg.err == nil && m.running() {
			m.confirmation = "restart-update"
		}
		return m, nil
	case restartStoppedMsg:
		cancelled := msg.ctx.Err() != nil
		if m.cancelWork != nil {
			m.cancelWork()
		}
		_ = m.sessionResult(sessionMsg{err: msg.err})
		if msg.err != nil {
			return m, nil
		}
		if cancelled {
			m.notice = "Restart cancelled. Connection stopped."
			return m, nil
		}
		return m, m.connect()
	case geoInfoMsg:
		if msg.seq == m.geoSeq && msg.dir == m.geoDir {
			m.geoFiles = msg.files
		}
		return m, nil
	case pathQueryMsg:
		return m, m.readPath(msg)
	case pathResultMsg:
		return m, m.pathResult(msg)
	case qrMsg:
		m.busy = false
		m.cancelWork = nil
		m.bad = msg.err != nil
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.qr = msg.modal
			m.notice = ""
		}
		return m, nil
	case engineVersionMsg:
		if msg.binary == m.config.EnginePath && msg.seq == m.coreVersionSeq {
			m.version = msg.version
			m.updates.current = update.ParseVersion(msg.version)
			if m.updates.current == "" {
				m.updates.current = "Unavailable"
			}
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.auth != nil {
			m.auth.input.SetWidth(max(8, min(32, msg.Width-22)))
		}
		if m.search != nil {
			m.search.input.SetWidth(max(12, msg.Width-8))
		}
		if m.form != nil {
			m.form.resize(msg.Width)
		}
		return m, nil
	case tickMsg:
		m.phase++
		m.snapshot = m.backend.Snapshot()
		m.observeTraffic(time.Time(msg))
		return m, tick()
	case actionMsg:
		return m, m.actionResult(msg)
	case sessionMsg:
		return m, m.sessionResult(msg)
	case prepareMsg:
		m.busy = false
		m.cancelWork = nil
		if msg.err != nil {
			m.bad = true
			m.notice = msg.err.Error()
			return m, nil
		}
		if msg.need {
			return m, m.beginAuthorization(msg.options)
		}
		return m, m.runStart(msg.options)
	case authorizationMsg:
		return m, m.authorizationResult(msg)
	case tea.KeyPressMsg:
		return m, m.handleKeyPress(msg)
	default:
		if m.auth != nil {
			return m, m.updateAuthorization(msg)
		}
		if m.search != nil {
			return m, m.updateSearch(msg)
		}
		if m.form != nil {
			return m, m.updateForm(msg)
		}
	}
	return m, nil
}

func (m *Model) actionResult(msg actionMsg) tea.Cmd {
	var refresh tea.Cmd
	m.downloads = nil
	m.busy = false
	m.cancelWork = nil
	m.bad = msg.err != nil
	if msg.err != nil {
		m.notice = msg.err.Error()
	} else {
		m.notice = msg.notice
		if msg.config != nil {
			changedCore := m.config.EnginePath != msg.config.EnginePath
			changedChannel := m.config.CoreUpdateChannel != msg.config.CoreUpdateChannel
			m.config = *msg.config
			m.ensureProfileCursor()
			m.info = msg.info
			if changedCore {
				m.version = "Checking…"
				refresh = m.loadVersion()
			}
			if changedChannel || changedCore {
				m.cancelUpdateCheck()
				m.updates.seq++
				m.updates.checking, m.updates.latest = false, nil
				m.updates.releases, m.updates.cursor = nil, 0
				m.updates.status = "Press r to check."
			}
		}
		if msg.closeForm {
			m.form = nil
		}
		if msg.version != "" {
			m.version = msg.version
		}
	}
	if msg.err == nil {
		if msg.geoDir != "" {
			refresh = tea.Batch(refresh, m.loadGeoInfo(msg.geoDir))
		} else if msg.config != nil {
			refresh = tea.Batch(refresh, m.loadGeoInfo(m.geoDirectory()))
		}
	}
	return refresh
}

func (m *Model) sessionResult(msg sessionMsg) tea.Cmd {
	m.auth = nil
	m.busy = false
	m.cancelWork = nil
	m.snapshot = m.backend.Snapshot()
	m.observeTraffic(time.Now())
	if msg.started && msg.err == nil && m.snapshot.State == session.Running {
		m.runningVersion = msg.version
		if update.ParseVersion(msg.version) == m.updates.current {
			m.updates.status = "Updated to " + m.updates.current + ". Active."
		}
	}
	if m.snapshot.State == session.Stopped || m.snapshot.State == session.Failed {
		m.runningVersion = ""
	}
	if msg.err == nil && m.updates.restartPending {
		m.updates.restartPending = false
		m.updates.status = "Updated to " + m.updates.current + ". Ready to connect."
		if m.snapshot.State == session.Running {
			m.updates.status = "Updated to " + m.updates.current + ". Active."
		}
	}
	m.bad = msg.err != nil
	if msg.err != nil {
		m.notice = msg.err.Error()
	} else if m.snapshot.State == session.Running {
		m.notice = "Connected. Open Logs to inspect engine output."
	} else {
		m.notice = "Disconnected."
	}
	if msg.quit {
		return tea.Quit
	}
	return nil
}
