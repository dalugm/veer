package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	update "github.com/dalugm/veer/engine/coreupdate"
)

func TestUpdaterHelpPreservesSelectionAndOperation(t *testing.T) {
	for _, busy := range []bool{false, true} {
		m, f := updaterModel(t)
		m.width, m.height = 70, 24
		m.updates.open, m.busy = true, busy
		m.updates.releases = []update.Release{{Version: "v1.2.0"}, {Version: "v1.1.0"}}
		m.updates.cursor = 1
		cancelled := false
		m.cancelWork = func() { cancelled = true }
		if view := ansi.Strip(m.View().Content); !strings.Contains(view, "? help") {
			t.Fatal("narrow updater has no help hint")
		}
		press(m, '?')
		if !m.showHelp {
			t.Fatal("updater did not open help")
		}
		view := ansi.Strip(m.View().Content)
		for _, text := range []string{"XRAY UPDATE", "Check releases", "Stable / Preview", "Confirm download", "Restore previous", "Cancel operation"} {
			if !strings.Contains(view, text) {
				t.Fatalf("missing %q in help:\n%s", text, view)
			}
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.showHelp || !m.updates.open || m.updates.cursor != 1 || m.busy != busy || cancelled ||
			f.checks != 0 ||
			f.installs != 0 {
			t.Fatal("closing help changed the updater or running operation")
		}
	}
}
