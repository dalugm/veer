package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	update "github.com/dalugm/veer/engine/coreupdate"
)

func TestSelectedVersionIsConfirmedAndInstalled(t *testing.T) {
	m, f := updaterModel(t)
	f.releases = []update.Release{{Version: "v1.2.0"}, {Version: "v1.1.0"}}
	press(m, 'u')
	runCommands(m, m.checkUpdate(true))
	press(m, 'j')
	press(m, 'u')
	view := ansi.Strip(m.View().Content)
	if m.confirmation != "update" || !strings.Contains(view, "v1.0.0 → v1.1.0") ||
		strings.Contains(view, "v1.2.0") {
		t.Fatalf("wrong confirmation:\n%s", view)
	}
	if f.installs != 0 {
		t.Fatal("navigation downloaded a release")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCommands(m, cmd)
	if f.installs != 1 || m.updates.current != "v1.1.0" {
		t.Fatal("selected older candidate was not installed")
	}
}

func TestVersionListScrollsAndResetsOnChannelChange(t *testing.T) {
	m, f := updaterModel(t)
	f.releases = nil
	for i := 12; i >= 1; i-- {
		f.releases = append(f.releases, update.Release{Version: fmt.Sprintf("v1.%d.0", i)})
	}
	press(m, 'u')
	runCommands(m, m.checkUpdate(true))
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "› v1.1.0") || !strings.Contains(view, "12/12") ||
			!strings.Contains(view, "Enter") {
			t.Fatalf("selection clipped at %v:\n%s", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("list overflow")
			}
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
		if m.updates.cursor != 0 {
			t.Fatal("home did not select first version")
		}
	}
	press(m, 'j')
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if len(m.updates.releases) != 0 || m.selectedUpdate() != nil {
		t.Fatal("channel change kept a stale selection")
	}
	runCommands(m, cmd)
	if m.updates.cursor != 0 {
		t.Fatal("fresh results did not reset selection")
	}
}
