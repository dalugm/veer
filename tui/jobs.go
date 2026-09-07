package tui

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// jobs fences asynchronous file/process operations when the terminal exits.
type jobs struct {
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func (j *jobs) track(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		j.mu.Lock()
		if j.closed {
			j.mu.Unlock()
			return actionMsg{err: context.Canceled}
		}
		j.wg.Add(1)
		j.mu.Unlock()
		defer j.wg.Done()
		return cmd()
	}
}
func (j *jobs) close() { j.mu.Lock(); j.closed = true; j.mu.Unlock(); j.wg.Wait() }

// Shutdown prevents queued operations from starting and waits for active work.
// Call after cancelling the application context and before stopping the backend.
func (m *Model) Shutdown() {
	if m.pathCancel != nil {
		m.pathCancel()
	}
	m.workers.close()
}
