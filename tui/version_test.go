package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	update "github.com/dalugm/veer/engine/coreupdate"
)

func TestManualReleaseCheckUpdatesVersionLabelsAcrossPages(t *testing.T) {
	m, f := updaterModel(t)
	m.version = "Xray 1.0.0"
	if got := m.coreLabel(); got != "Xray 1.0.0 · Unknown" {
		t.Fatalf("unverified numeric release: %s", got)
	}
	runCommands(m, m.checkUpdate())
	if f.checks != 1 {
		t.Fatal("manual check did not complete")
	}
	for _, page := range []Page{Overview, Settings} {
		m.page = page
		if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Xray 1.0.0 · Stable") {
			t.Fatalf("page %s missing release label:\n%s", pageNames[page], view)
		}
	}
	m.page, m.showDetails = Overview, true
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Xray 1.0.0 · Stable") {
		t.Fatalf("details missing release label:\n%s", view)
	}
	if f.checks != 1 {
		t.Fatal("viewing release labels triggered another check")
	}
}

func TestReleaseLabelsKeepRunningAndInstalledVersionsSeparate(t *testing.T) {
	m, _ := restartModel(t)
	m.Update(updateInstalledMsg{
		channel: update.Preview,
		result:  update.Result{Version: "v1.1.0", VersionOutput: "Xray 1.1.0"},
	})
	press(m, 'n')
	m.page = Overview
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Xray 1.0.0 · Stable") ||
		strings.Contains(view, "Xray 1.1.0 · Preview") {
		t.Fatalf("running version was relabeled after download:\n%s", view)
	}
	m.page = Settings
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Xray 1.1.0 · Preview") {
		t.Fatalf("settings do not show installed version:\n%s", view)
	}
	// A failed metadata lookup must not erase a previously verified release.
	m.rememberVersionChannel(m.version, "")
	if got := m.versionChannelLabel(m.version); got != "Preview" {
		t.Fatal("lost verified release after a failed lookup: " + got)
	}
	// Offline backup recovery can reuse this session's known release metadata.
	m.updateInstalled(updateInstalledMsg{result: update.Result{
		Version: "v1.0.0", VersionOutput: "Xray 1.0.0",
	}})
	if got := m.versionChannelLabel(m.version); got != "Stable" {
		t.Fatal("lost restored release label: " + got)
	}
}
