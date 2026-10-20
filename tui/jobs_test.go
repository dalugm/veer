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

func TestResumingAfterFailedQuitRejectsOldQueuedWork(t *testing.T) {
	var jobs jobs
	oldRan, closedRan, newRan := false, false, false
	old := jobs.track(func() tea.Msg { oldRan = true; return nil })
	jobs.close()
	closed := jobs.track(func() tea.Msg { closedRan = true; return nil })
	jobs.resume()
	old()
	closed()
	jobs.track(func() tea.Msg { newRan = true; return nil })()
	if oldRan || closedRan || !newRan {
		t.Fatalf("old=%v closed=%v new=%v", oldRan, closedRan, newRan)
	}
}
