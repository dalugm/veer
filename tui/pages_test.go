package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/settings"
)

func TestPagesRemainSeparateAtEverySize(t *testing.T) {
	m := newTestModel(t)
	m.config.Profiles = []settings.Profile{
		{ID: "one", Name: "Home", Path: "/profiles/home.json", Engine: "xray"},
	}
	m.config.Selected = "one"
	m.snapshot.Logs = []string{"log-only-marker"}
	for _, size := range [][2]int{{160, 48}, {120, 36}, {80, 24}, {60, 18}} {
		m.page = Overview
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		if strings.Contains(view, "ENGINE LOGS") || strings.Contains(view, "log-only-marker") ||
			strings.Contains(view, "/profiles/home.json") {
			t.Fatal("overview contains secondary detail")
		}
		if !strings.Contains(view, "TRAFFIC") || !strings.Contains(view, "Home") ||
			!strings.Contains(view, "q quit") {
			t.Fatal(view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("overflow")
			}
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if m.page != Profiles {
			t.Fatal("Tab must switch pages at every size")
		}
		if !strings.Contains(m.View().Content, "/profiles/home.json") {
			t.Fatal("focused path missing")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if m.page != Overview {
			t.Fatal("Shift+Tab must return to Overview")
		}
	}
}

func TestProfileSearchUsesOriginalIndices(t *testing.T) {
	m := newTestModel(t)
	m.page = Profiles
	m.config.Profiles = []settings.Profile{
		{ID: "a", Name: "Home"},
		{ID: "b", Name: "Work"},
		{ID: "c", Name: "Weekend"},
	}
	press(m, '/')
	for _, r := range "we" {
		press(m, r)
	}
	if m.cursor != 2 || len(m.visibleProfiles()) != 1 {
		t.Fatal("search did not find original profile")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no selection action")
	}
	m.Update(cmd())
	if m.config.Selected != "c" {
		t.Fatal("selected hidden profile")
	}
	press(m, '/')
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	for _, r := range "missing" {
		press(m, r)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.cursor != -1 {
		t.Fatal("empty results have an actionable cursor")
	}
	for _, r := range "edy" {
		_, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		if cmd != nil || m.form != nil || m.confirmation != "" {
			t.Fatal("empty result acted on hidden profile")
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.visibleProfiles()) != 3 {
		t.Fatal("escape did not clear filter")
	}
	press(m, '/')
	press(m, 'q')
	if m.quitting {
		t.Fatal("search swallowed normal text as a command")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.profileFilter != "" {
		t.Fatal("cancel did not restore filter")
	}
}

func TestStructuredLogsWrapAndSanitize(t *testing.T) {
	got := parseLog("2026/09/08 12:34:56.123 [Warning] [123] proxy/vless: hello")
	if got.time != "12:34:56" || got.level != "WARN" || got.source != "proxy/vless" ||
		got.message != "[123] hello" {
		t.Fatalf("parsed: %#v", got)
	}
	raw := parseLog("unrecognized output stays visible")
	if raw.message != "unrecognized output stays visible" {
		t.Fatal("lost raw output")
	}
	m := newTestModel(t)
	m.snapshot.Logs = []string{
		"2026/09/08 12:34:56 [Error] proxy: " + strings.Repeat("中文消息", 20) + "\x1b[2J",
	}
	rows := m.logLines(56)
	if len(rows) < 2 {
		t.Fatal("long line not wrapped")
	}
	for i, row := range rows {
		clean := ansi.Strip(row)
		if ansi.StringWidth(row) > 56 || strings.Contains(row, "\x1b[2J") {
			t.Fatal("unsafe or overflowing row")
		}
		if i > 0 && !strings.HasPrefix(clean, strings.Repeat(" ", logPrefixWidth)) {
			t.Fatal("continuation not aligned")
		}
	}
}

func TestHelpClosesWithoutQuitting(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 60, 18
	m.page = Profiles
	press(m, '?')
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"KEYBOARD", "/ search", "e rename", "Esc / q"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing help %q\n%s", want, view)
		}
	}
	press(m, 'q')
	if m.showHelp || m.quitting {
		t.Fatal("q should close help only")
	}
}

func TestConnectionDetailsAreExplicit(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 60, 18
	path := "/profiles/" + strings.Repeat("long-directory/", 15) + "home.json"
	m.config.Profiles = []settings.Profile{{ID: "one", Name: "Home", Path: path, Engine: "xray"}}
	m.config.Selected = "one"
	press(m, 'i')
	if !m.showDetails {
		t.Fatal("missing details")
	}
	press(m, 'G')
	if !strings.Contains(ansi.Strip(m.View().Content), "home.json") {
		t.Fatal("path tail inaccessible")
	}
	press(m, 'q')
	if m.showDetails || m.quitting {
		t.Fatal("q must close details only")
	}
}
