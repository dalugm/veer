package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	update "github.com/dalugm/veer/engine/coreupdate"
	"github.com/dalugm/veer/settings"
)

type releaseUpdater interface {
	Check(context.Context, string, update.Channel) ([]update.Release, error)
	Install(context.Context, string, string, update.Release) (update.Result, error)
	Restore(context.Context, string, string, string) (update.Result, error)
}

type appUpdate struct {
	client                          releaseUpdater
	current, status, backup, binary string
	readVersion                     func(context.Context, string) (string, error)
	latest                          *update.Release
	releases                        []update.Release
	cursor                          int
	open, checking                  bool
	restartPending                  bool
	seq                             uint64
	cancel                          context.CancelFunc
}

type updateCheckedMsg struct {
	seq             uint64
	channel         update.Channel
	manual          bool
	binary, version string
	releases        []update.Release
	err             error
}

type updateInstalledMsg struct {
	result  update.Result
	err     error
	saveErr error
}

func (m *Model) cancelUpdateCheck() {
	if m.updates.cancel != nil {
		m.updates.cancel()
		m.updates.cancel = nil
	}
}

func (m *Model) checkUpdate(manual bool) tea.Cmd {
	if m.updates.client == nil {
		return nil
	}
	m.cancelUpdateCheck()
	m.updates.seq++
	m.updates.latest = nil
	m.updates.releases, m.updates.cursor = nil, 0
	m.updates.checking, m.updates.status = true, "Checking GitHub releases…"
	ctx, cancel := context.WithCancel(m.ctx)
	m.updates.cancel = cancel
	seq, channel := m.updates.seq, update.Channel(m.config.CoreUpdateChannel)
	client, binary, read := m.updates.client, m.config.EnginePath, m.updates.readVersion
	return m.workers.track(func() tea.Msg {
		defer cancel()
		v, err := read(ctx, binary)
		var releases []update.Release
		if err == nil {
			releases, err = client.Check(ctx, v, channel)
		}
		return updateCheckedMsg{
			seq:      seq,
			channel:  channel,
			manual:   manual,
			releases: releases,
			err:      err,
			binary:   binary,
			version:  v,
		}
	})
}

func (m *Model) updateChecked(msg updateCheckedMsg) {
	if msg.seq != m.updates.seq || string(msg.channel) != m.config.CoreUpdateChannel ||
		msg.binary != m.config.EnginePath {
		return
	}
	m.cancelUpdateCheck()
	m.updates.checking, m.updates.latest = false, nil
	m.updates.releases, m.updates.cursor = msg.releases, 0
	if msg.err != nil {
		m.updates.releases = nil
	}
	if len(m.updates.releases) > 0 {
		m.updates.latest = &m.updates.releases[0]
	}
	m.updates.binary = msg.binary
	m.updates.current = update.ParseVersion(msg.version)
	if m.updates.current == "" {
		m.updates.current = "Unavailable"
	}
	if msg.version != "" {
		m.version = msg.version
	}
	switch {
	case errors.Is(msg.err, update.ErrUnknownVersion):
		m.updates.status = "Check the Xray path/version in Settings."
	case errors.Is(msg.err, context.Canceled):
		m.updates.status = "Update check cancelled."
	case msg.err != nil:
		m.updates.status = "Check failed: " + msg.err.Error()
	case m.updates.latest != nil:
		m.updates.status = "Choose a version to install."
	case update.Channel(m.config.CoreUpdateChannel) == update.Stable:
		m.updates.status = "No newer stable release."
	default:
		m.updates.status = "No newer release."
	}
	if msg.manual && m.updates.open && !m.busy && m.confirmation == "" {
		m.bad, m.notice = msg.err != nil, m.updates.status
	}
}

func (m *Model) updateKey(key string) tea.Cmd {
	if m.busy {
		if key == "esc" && m.cancelWork != nil {
			m.cancelWork()
			m.notice = "Cancelling…"
		}
		return nil
	}
	switch key {
	case "esc", "q":
		m.cancelUpdateCheck()
		if m.updates.checking {
			m.updates.seq++
			m.updates.checking, m.updates.status = false, "Update check cancelled."
		}
		m.updates.open = false
	case "r":
		if !m.updates.checking {
			return m.checkUpdate(true)
		}
	case "b":
		if m.availableBackup() != "" {
			m.confirmation = "restore-update"
		}
	case "h", "l", "left", "right":
		c := m.config
		c.CoreUpdateChannel = "stable"
		if key == "l" || key == "right" {
			c.CoreUpdateChannel = "preview"
		}
		if c.CoreUpdateChannel == m.config.CoreUpdateChannel {
			return nil
		}
		m.cancelUpdateCheck()
		m.updates.seq++
		m.updates.checking, m.updates.latest = false, nil
		m.updates.releases, m.updates.cursor = nil, 0
		m.updates.status = "Press r to check."
		m.begin("Saving update channel…")
		return m.save(c, "", false)
	case "j", "down":
		m.moveUpdateSelection(1)
	case "k", "up":
		m.moveUpdateSelection(-1)
	case "home":
		m.moveUpdateSelection(-len(m.updates.releases))
	case "end":
		m.moveUpdateSelection(len(m.updates.releases))
	case "pgdown", "ctrl+f":
		m.moveUpdateSelection(max(1, m.height-17))
	case "pgup", "ctrl+b":
		m.moveUpdateSelection(-max(1, m.height-17))
	case "enter", "u":
		if m.updates.checking || m.selectedUpdate() == nil {
			return nil
		}
		m.confirmation = "update"
	}
	return nil
}

func (m *Model) installUpdate() tea.Cmd {
	if m.quitting || m.busy || m.selectedUpdate() == nil ||
		m.updates.checking {
		return nil
	}
	if m.updates.binary != m.config.EnginePath {
		return nil
	}
	ctx, cancel := m.begin("Downloading and verifying Xray…")
	m.cancelUpdateCheck()
	m.updates.seq++
	client, current, binary, release := m.updates.client, m.updates.current, m.config.EnginePath, *m.selectedUpdate()
	config, path := m.config, m.path
	return m.workers.track(func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return updateInstalledMsg{err: err}
		}
		result, err := client.Install(ctx, binary, current, release)
		return persistCoreUpdate(result, err, config, path)
	})
}

func persistCoreUpdate(
	result update.Result,
	err error,
	config settings.Config,
	path string,
) updateInstalledMsg {
	msg := updateInstalledMsg{result: result, err: err}
	if err == nil {
		config.CoreBackupPath, config.CoreBackupFor = result.BackupPath, config.EnginePath
		config.CoreBackupTarget = result.TargetPath
		msg.saveErr = settings.Save(path, config)
	}
	return msg
}

func (m *Model) availableBackup() string {
	if m.config.CoreBackupFor == m.config.EnginePath {
		return m.config.CoreBackupPath
	}
	return ""
}

func (m *Model) restoreUpdate() tea.Cmd {
	backup := m.availableBackup()
	if m.quitting || m.busy || backup == "" {
		return nil
	}
	m.cancelUpdateCheck()
	m.updates.seq++
	m.updates.checking = false
	ctx, cancel := m.begin("Restoring previous Xray…")
	client, config, path := m.updates.client, m.config, m.path
	return m.workers.track(func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return updateInstalledMsg{err: err}
		}
		result, err := client.Restore(ctx, config.EnginePath, backup, config.CoreBackupTarget)
		return persistCoreUpdate(result, err, config, path)
	})
}

func (m *Model) updateInstalled(msg updateInstalledMsg) {
	m.busy, m.cancelWork, m.bad = false, nil, msg.err != nil
	if msg.err != nil {
		m.notice = "Update failed: " + msg.err.Error()
		m.updates.status = m.notice
		return
	}
	m.updates.current, m.updates.backup = msg.result.Version, msg.result.BackupPath
	m.config.CoreBackupPath, m.config.CoreBackupFor = msg.result.BackupPath, m.config.EnginePath
	m.config.CoreBackupTarget = msg.result.TargetPath
	m.coreVersionSeq++
	m.version = msg.result.VersionOutput
	if m.version == "" {
		m.version = "Xray " + strings.TrimPrefix(msg.result.Version, "v")
	}
	m.updates.latest = nil
	m.updates.releases, m.updates.cursor = nil, 0
	m.updates.status = "Updated to " + msg.result.Version + ". Ready to connect."
	m.updates.restartPending = m.running()
	if m.updates.restartPending {
		m.updates.status = "Updated to " + msg.result.Version + ". Restart connection to apply."
		m.notice = "Xray updated. Current connection is unchanged; restart it to apply."
	} else {
		m.notice = "Xray updated. The next connection will use the new core."
	}
	if msg.saveErr != nil {
		m.bad = true
		m.notice = "Core changed, but backup settings could not be saved: " + msg.saveErr.Error() + ". Backup: " + msg.result.BackupPath
	}
}

func (m *Model) updateView(w, h int) string {
	stable, preview := "○ Stable", "○ Preview"
	if m.config.CoreUpdateChannel == "preview" {
		preview = "● Preview"
	} else {
		stable = "● Stable"
	}
	lines := []string{
		"Xray " + safe(m.updates.current),
		accent.Render(stable + "    " + preview),
		safe(m.updates.status),
	}
	if m.availableBackup() != "" {
		lines[0] += " · b restore previous"
	}
	count := len(m.updates.releases)
	if count > 0 {
		rows := max(1, h-9)
		start := max(0, min(m.updates.cursor-rows+1, count-rows))
		for i := start; i < min(count, start+rows); i++ {
			r := m.updates.releases[i]
			label := "Stable"
			if r.Prerelease {
				label = "Preview"
			}
			line := "  " + safe(r.Version) + "  " + label
			if i == m.updates.cursor {
				line = accent.Render("› " + safe(r.Version) + "  " + label)
			}
			lines = append(lines, line)
		}
		lines = append(
			lines,
			fmt.Sprintf("Version %d/%d · newer than current", m.updates.cursor+1, count),
			"j/k choose · r check · Enter install",
		)
	} else {
		lines = append(
			lines,
			"",
			"r check · Enter download and install",
			"Stable: releases · Preview: releases + prereleases",
		)
	}

	if m.updates.backup != "" && h >= 14 {
		lines = append(lines, "", "Previous core backup:", safe(m.updates.backup))
	}

	return box("XRAY UPDATE", strings.Join(lines, "\n"), w, h)
}

func (m *Model) selectedUpdate() *update.Release {
	if m.updates.cursor < 0 || m.updates.cursor >= len(m.updates.releases) {
		return nil
	}
	return &m.updates.releases[m.updates.cursor]
}

func (m *Model) moveUpdateSelection(delta int) {
	if len(m.updates.releases) == 0 {
		return
	}
	m.updates.cursor = max(0, min(len(m.updates.releases)-1, m.updates.cursor+delta))
}
