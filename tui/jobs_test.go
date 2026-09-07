package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestShutdownRejectsQueuedFileWork(t *testing.T) {
	var jobs jobs
	jobs.close()
	ran := false
	cmd := jobs.track(func() tea.Msg { ran = true; return nil })
	cmd()
	if ran {
		t.Fatal("queued operation started after shutdown")
	}
}
