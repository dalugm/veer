package tui

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// jobs fences asynchronous file/process operations when the terminal exits.
type jobs struct {
	mu     sync.Mutex
	closed bool
	epoch  uint64
	wg     sync.WaitGroup
}

func (j *jobs) track(cmd tea.Cmd) tea.Cmd {
	j.mu.Lock()
	epoch := j.epoch
	closed := j.closed
	j.mu.Unlock()
	return func() tea.Msg {
		j.mu.Lock()
		if closed || j.closed || epoch != j.epoch {
			j.mu.Unlock()
			return nil
		}
		j.wg.Add(1)
		j.mu.Unlock()
		defer j.wg.Done()
		return cmd()
	}
}

func (j *jobs) close() {
	j.mu.Lock()
	j.closed = true
	j.epoch++
	j.mu.Unlock()
	j.wg.Wait()
}

// resume accepts new work after a failed quit. Previously queued work remains fenced.
func (j *jobs) resume() {
	j.mu.Lock()
	j.closed = false
	j.mu.Unlock()
}

// Shutdown prevents queued operations from starting and waits for active work.
// Call after cancelling the application context and before stopping the backend.
func (m *Model) Shutdown() {
	m.clearArchiveSearch()
	m.cancelUpdateCheck()
	if m.pathCancel != nil {
		m.pathCancel()
	}
	m.workers.close()
}
