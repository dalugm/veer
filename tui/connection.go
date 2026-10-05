package tui

import (
	"context"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/privilege"
)

func (m *Model) connect() tea.Cmd {
	p, ok := m.config.Active()
	if !ok {
		m.page = Profiles
		m.openImport()
		return nil
	}
	if p.Engine != "xray" {
		m.bad = true
		m.notice = "This version supports Xray profiles only."
		return nil
	}
	if m.running() {
		m.bad = true
		m.notice = "Disconnect the current session first."
		return nil
	}
	o := engine.Options{
		Binary:         m.config.EnginePath,
		Config:         p.Path,
		GeoDir:         m.config.GeoDir,
		NetworkService: m.config.NetworkService,
	}
	dns, err := m.config.DNSServers()
	if err != nil {
		m.bad = true
		m.notice = err.Error()
		return nil
	}
	if runtime.GOOS == "windows" && m.config.DNSMode != "custom" {
		dns = nil
	}
	o.DNS = dns
	ctx, cancel := m.begin("Inspecting configuration…")
	return m.workers.track(func() tea.Msg {
		defer cancel()
		need, err := privilege.NeedsElevation(o)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return prepareMsg{options: o, need: need, err: err}
	})
}

func (m *Model) runStart(o engine.Options) tea.Cmd {
	m.logOffset = 0
	m.clearArchiveSearch()
	ctx, cancel := m.begin("Starting Xray…")
	read := m.updates.readVersion
	return m.workers.track(func() tea.Msg {
		version, versionErr := read(ctx, o.Binary)
		if versionErr != nil || version == "" {
			version = "Unavailable"
		}
		if err := ctx.Err(); err != nil {
			cancel()
			return sessionMsg{err: err}
		}
		err := m.backend.Start(ctx, o)
		if err != nil {
			cancel()
		}
		return sessionMsg{err: err, started: true, version: version}
	})
}

func (m *Model) restart() tea.Cmd {
	if m.quitting || m.busy {
		return nil
	}
	if !m.running() {
		return m.connect()
	}
	if _, ok := m.config.Active(); !ok {
		m.bad, m.notice = true, "Choose a profile before restarting."
		return nil
	}
	ctx, _ := m.begin("Restarting Xray…")
	return m.workers.track(func() tea.Msg {
		if err := ctx.Err(); err != nil {
			return restartStoppedMsg{err: err, ctx: ctx}
		}
		// Finish process shutdown and DNS rollback even if restarting is cancelled.
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return restartStoppedMsg{err: m.backend.Stop(stopCtx), ctx: ctx}
	})
}

func (m *Model) stop(quit bool) tea.Cmd {
	m.busy = true
	m.busyLabel = "Stopping Xray…"
	m.notice = m.busyLabel
	return func() tea.Msg {
		if quit {
			m.workers.close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return sessionMsg{err: m.backend.Stop(ctx), quit: quit}
	}
}

func (m *Model) quit() tea.Cmd {
	m.cancelUpdateCheck()
	m.clearPathCompletion()
	m.quitting = true
	if m.auth != nil {
		m.auth.input.SetValue("")
	}
	if m.cancelWork != nil {
		m.cancelWork()
	}
	return m.stop(true)
}
