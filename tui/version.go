package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	update "github.com/dalugm/veer/engine/coreupdate"
)

func (m *Model) checkVersion() tea.Cmd {
	ctx, cancel := m.begin("Checking Xray version…")
	binary := m.config.EnginePath
	return m.workers.track(func() tea.Msg {
		defer cancel()
		v, err := readVersion(ctx, binary)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{version: v, notice: v}
	})
}

func (m *Model) loadVersion() tea.Cmd {
	m.coreVersionSeq++
	seq := m.coreVersionSeq
	ctx, binary := m.ctx, m.config.EnginePath
	return m.workers.track(func() tea.Msg {
		v, err := readVersion(ctx, binary)
		if err != nil {
			v = "Unavailable"
		}
		return engineVersionMsg{binary: binary, version: v, seq: seq}
	})
}

func readVersion(ctx context.Context, binary string) (string, error) {
	return engine.Version(ctx, binary)
}

// Keep release status by version so installing a new core does not relabel
// a running connection. Only explicit checks and installations populate it.
func (m *Model) rememberVersionChannel(output string, channel update.Channel) {
	version := update.ParseVersion(output)
	if version == "" || (channel != update.Stable && channel != update.Preview) {
		return
	}
	if m.versionChannels == nil {
		m.versionChannels = make(map[string]update.Channel)
	}
	m.versionChannels[version] = channel
}

func (m *Model) versionChannelLabel(output string) string {
	channel := update.LocalChannel(output)
	if channel == "" {
		channel = m.versionChannels[update.ParseVersion(output)]
	}
	switch channel {
	case update.Stable:
		return "Stable"
	case update.Preview:
		return "Preview"
	default:
		return "Unknown"
	}
}

func (m *Model) coreLabel() string {
	version := m.version
	if m.running() {
		version = m.runningVersion
		if version == "" {
			version = "Unavailable"
		}
	}
	return m.versionLabel(version)
}

func (m *Model) versionLabel(version string) string {
	if parsed := update.ParseVersion(version); parsed != "" {
		return "Xray " + strings.TrimPrefix(parsed, "v") + " · " + m.versionChannelLabel(version)
	}
	fields := strings.Fields(safe(version))
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Xray") {
		return "Xray " + fields[1]
	}
	return "Xray · " + safe(version)
}
