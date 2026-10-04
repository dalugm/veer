package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
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
