package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func appendArchiveLines(t *testing.T, path string, first, end int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for i := first; i < end; i++ {
		if _, err := fmt.Fprintf(f, "needle %04d\n", i); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScrolledArchiveKeepsItsPageAndPaginationWhileStreaming(t *testing.T) {
	for _, tc := range []struct{ skip, offset int }{{0, 42}, {400, 42}, {400, 0}} {
		t.Run(fmt.Sprintf("skip%d-offset%d", tc.skip, tc.offset), func(t *testing.T) {
			m := newTestModel(t)
			m.page, m.width, m.height = Logs, 120, 30
			path := filepath.Join(t.TempDir(), "session.log")
			appendArchiveLines(t, path, 0, 1001)
			m.snapshot.LogArchive, m.snapshot.LogCount, m.logFilter = path, 1001, "needle"
			m.Update(m.searchArchive(tc.skip)())
			m.logOffset = tc.offset
			before := m.logBody(m.logWidth(), m.navigationRows()+1)
			appendArchiveLines(t, path, 1001, 1011)
			m.snapshot.LogCount = 1011
			if cmd := m.refreshArchiveSearch(); cmd != nil {
				m.Update(cmd())
			}
			if m.logBody(m.logWidth(), m.navigationRows()+1) != before {
				t.Fatal("new matches moved the page being read")
			}
			apply := func(cmd tea.Cmd) {
				t.Helper()
				if cmd == nil {
					t.Fatal("missing archive page command")
				}
				m.Update(cmd())
			}
			apply(m.pageKey("home"))
			appendArchiveLines(t, path, 1011, 1021)
			m.snapshot.LogCount = 1021
			var matches []string
			for {
				matches = append(matches, m.logSearch.result.Lines...)
				if m.logSearch.result.Skip == 0 {
					break
				}
				m.logOffset = 0
				apply(m.pageKey("j"))
			}
			if len(matches) != 1001 {
				t.Fatalf("pagination changed its snapshot: got %d matches", len(matches))
			}
			for i, line := range matches {
				if line != fmt.Sprintf("needle %04d", i) {
					t.Fatalf("match %d was skipped or repeated: %q", i, line)
				}
			}
			apply(m.pageKey("G"))
			if m.logOffset != 0 || m.logSearch.result.Skip != 0 ||
				m.logSearch.result.Matches != 1021 ||
				!strings.Contains(m.logBody(m.logWidth(), m.navigationRows()+1), "needle 1020") {
				t.Fatal("G did not resume following the latest matches")
			}
		})
	}
}

func TestScrollingDiscardsAnInflightArchiveRefresh(t *testing.T) {
	m := newTestModel(t)
	m.page, m.width, m.height = Logs, 120, 30
	path := filepath.Join(t.TempDir(), "session.log")
	appendArchiveLines(t, path, 0, 1001)
	m.snapshot.LogArchive, m.snapshot.LogCount, m.logFilter = path, 1001, "needle"
	m.Update(m.searchArchive(0)())
	appendArchiveLines(t, path, 1001, 1011)
	m.snapshot.LogCount = 1011
	pending := m.refreshArchiveSearch()
	if pending == nil {
		t.Fatal("live search did not refresh")
	}
	// The scan may already have completed while its result waits in Tea's queue.
	result := pending()
	press(m, 'k')
	before := m.logBody(m.logWidth(), m.navigationRows()+1)
	m.Update(result)
	if m.logSearch.loading || m.logSearch.result.Matches != 1001 ||
		m.logBody(m.logWidth(), m.navigationRows()+1) != before {
		t.Fatal("an earlier live refresh replaced the scrolled page")
	}
	if m.refreshArchiveSearch() != nil {
		t.Fatal("a paused search started refreshing again")
	}
}
