package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/session"
)

func TestLogsSearchApplyCancelAndClear(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	m.snapshot.Logs = []string{"[access] accepted example.com", "[error] TLS timeout"}
	m.pageKey("/")
	m.updateSearch(tea.KeyPressMsg{Code: 't', Text: "TLS"})
	if m.logFilter != "TLS" || len(m.logLines(120)) != 1 {
		t.Fatalf("filter=%q", m.logFilter)
	}
	m.updateSearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.search != nil || !strings.Contains(ansi.Strip(m.logsView(120, 20)), "1/2") {
		t.Fatal("search not applied")
	}
	m.logOffset = 3
	m.pageKey("/")
	m.updateSearch(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.logFilter != "" {
		t.Fatal("Ctrl+U did not clear")
	}
	m.updateSearch(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.logFilter != "TLS" || m.logOffset != 3 {
		t.Fatal("Esc did not restore search and scroll")
	}
	m.pageKey("esc")
	if m.logFilter != "" || m.logOffset != 0 {
		t.Fatal("Esc did not clear applied search")
	}
}

func TestLogsFilterIsLiteralAndFollowsNewMatches(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	m.logFilter = "[ERROR]"
	m.snapshot.Logs = []string{"[error] first", "unrelated"}
	if len(m.logLines(120)) != 1 {
		t.Fatal("filter must be literal and case insensitive")
	}
	m.snapshot.Logs = append(m.snapshot.Logs, "[ERROR] second")
	if len(m.logLines(120)) != 2 {
		t.Fatal("new matches not included")
	}
	m.logFilter = "absent"
	if !strings.Contains(m.logBody(120, 20), "No matching logs") {
		t.Fatal("empty match state missing")
	}
}

func TestArchiveSearchFindsOlderLogsAndNavigatesPages(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	m.width, m.height = 120, 30
	path := filepath.Join(t.TempDir(), "session.log")
	var data strings.Builder
	for i := range 1001 {
		fmt.Fprintf(&data, "needle %04d\n", i)
	}
	if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	m.snapshot.LogArchive, m.snapshot.LogCount = path, 1001
	m.logFilter = "needle"
	apply := func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("missing search command")
		}
		m.Update(cmd())
	}
	apply(m.searchArchive(0))
	if m.logSearch.result.Matches != 1001 ||
		!strings.Contains(strings.Join(m.logSearch.result.Lines, "\n"), "needle 0601") {
		t.Fatal("archive not searched")
	}
	apply(m.pageKey("home"))
	if m.logSearch.result.Lines[0] != "needle 0000" || m.logOffset == 0 {
		t.Fatal("Home did not reach oldest matches")
	}
	apply(m.pageKey("G"))
	if m.logSearch.result.Skip != 0 || m.logOffset != 0 {
		t.Fatal("G did not follow latest matches")
	}
	m.logOffset = max(0, len(m.logLines(m.logWidth()))-m.navigationRows())
	apply(m.pageKey("k"))
	if m.logSearch.result.Skip != 400 || m.logSearch.result.Lines[0] != "needle 0201" {
		t.Fatal("cannot scroll into older page")
	}
	m.logOffset = 0
	apply(m.pageKey("j"))
	if m.logSearch.result.Skip != 0 || m.logOffset == 0 {
		t.Fatal("cannot scroll back into newer page")
	}
	previous := m.logSearch.seq
	m.logFilter = "unmatched"
	apply(m.searchArchive(0))
	m.Update(
		archiveSearchMsg{seq: previous, result: session.LogSearchResult{Lines: []string{"stale"}}},
	)
	if len(m.logSearch.result.Lines) != 0 {
		t.Fatal("stale search replaced current results")
	}
}

func TestUnchangedLogSearchInputPreservesRunningScan(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	path := filepath.Join(t.TempDir(), "session.log")
	if err := os.WriteFile(path, []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.snapshot.LogArchive, m.snapshot.LogCount = path, 1
	m.logFilter = "needle"
	pending := m.searchArchive(0)
	seq := m.logSearch.seq
	cancelled := false
	cancel := m.logSearch.cancel
	m.logSearch.cancel = func() { cancelled = true; cancel() }
	m.startSearch()
	for _, msg := range []tea.Msg{cursor.BlinkMsg{}, cursor.BlinkMsg{}, tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
		m.Update(msg)
		if cancelled || m.logSearch.seq != seq {
			t.Fatal("unchanged input restarted the running scan")
		}
	}
	m.Update(pending())
	if !m.logSearch.ready || m.logSearch.result.Matches != 1 {
		t.Fatal("running search could not finish")
	}
}

func TestCancelLogSearchRestoresArchivePageAndOffset(t *testing.T) {
	for _, edit := range []string{"unchanged", "changed", "reverted"} {
		t.Run(edit, func(t *testing.T) {
			m := newTestModel(t)
			m.page = Logs
			m.width, m.height = 120, 30
			path := filepath.Join(t.TempDir(), "session.log")
			var data strings.Builder
			for i := range 1001 {
				fmt.Fprintf(&data, "needle %04d\n", i)
			}
			if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			m.snapshot.LogArchive, m.snapshot.LogCount = path, 1001
			m.logFilter = "needle"
			m.Update(m.searchArchive(400)())
			m.logOffset = 42
			m.startSearch()
			if edit != "unchanged" {
				m.updateSearch(tea.KeyPressMsg{Code: 'x', Text: "x"})
				if edit == "reverted" {
					m.updateSearch(tea.KeyPressMsg{Code: tea.KeyBackspace})
				}
			}
			if cmd := m.updateSearch(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
				m.Update(cmd())
			}
			if m.logFilter != "needle" || m.logOffset != 42 || m.logSearch.result.Skip != 400 ||
				m.logSearch.result.Lines[0] != "needle 0201" {
				t.Fatalf(
					"did not restore page: filter=%q offset=%d result=%+v",
					m.logFilter,
					m.logOffset,
					m.logSearch.result,
				)
			}
		})
	}
}

func TestHomeThenForwardPaginationDoesNotRepeatMatches(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	m.width, m.height = 120, 30
	path := filepath.Join(t.TempDir(), "session.log")
	var data strings.Builder
	for i := range 1001 {
		fmt.Fprintf(&data, "needle %04d\n", i)
	}
	if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	m.snapshot.LogArchive, m.snapshot.LogCount = path, 1001
	m.logFilter = "needle"
	m.Update(m.searchArchive(0)())
	m.Update(m.pageKey("home")())
	var matches []string
	for {
		matches = append(matches, m.logSearch.result.Lines...)
		if m.logSearch.result.Skip == 0 {
			break
		}
		m.logOffset = 0
		cmd := m.pageKey("j")
		if cmd == nil {
			t.Fatal("forward pagination did not load next page")
		}
		m.Update(cmd())
	}
	if len(matches) != 1001 {
		t.Fatalf("got %d results; want 1001", len(matches))
	}
	for i, line := range matches {
		if line != fmt.Sprintf("needle %04d", i) {
			t.Fatalf("match %d = %q", i, line)
		}
	}
}
