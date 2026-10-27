package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/session"
)

type archiveSearch struct {
	seq            uint64
	cancel         context.CancelFunc
	loading, ready bool
	top            bool
	path, query    string
	count          uint64
	result         session.LogSearchResult
}

type archiveSearchMsg struct {
	seq    uint64
	count  uint64
	result session.LogSearchResult
	err    error
}

func (m *Model) clearArchiveSearch() {
	if m.logSearch.cancel != nil {
		m.logSearch.cancel()
	}
	seq := m.logSearch.seq + 1
	m.logSearch = archiveSearch{seq: seq}
}

func (m *Model) searchArchive(skip int) tea.Cmd {
	return m.searchArchivePage(session.LogSearchOptions{Skip: skip}, m.snapshot.LogCount)
}

func (m *Model) searchArchivePage(options session.LogSearchOptions, count uint64) tea.Cmd {
	if strings.TrimSpace(m.logFilter) == "" || m.snapshot.LogArchive == "" {
		m.clearArchiveSearch()
		return nil
	}
	if m.logSearch.cancel != nil {
		m.logSearch.cancel()
	}
	m.logSearch.seq++
	path, query := m.snapshot.LogArchive, m.logFilter
	if m.logSearch.path != path || m.logSearch.query != query {
		m.logSearch.ready = false
	}
	m.logSearch.path, m.logSearch.query = path, query
	m.logSearch.loading = true
	m.logSearch.top = false
	ctx, cancel := context.WithCancel(m.ctx)
	m.logSearch.cancel = cancel
	seq := m.logSearch.seq
	return m.workers.track(func() tea.Msg {
		defer cancel()
		result, err := session.SearchLogs(ctx, path, query, options)
		return archiveSearchMsg{seq: seq, count: count, result: result, err: err}
	})
}

func (m *Model) refreshArchiveSearch() tea.Cmd {
	if m.page != Logs || m.logFilter == "" || m.snapshot.LogArchive == "" || m.logSearch.loading {
		return nil
	}
	// Scrolled pages retain their file boundary, including across pagination.
	// Returning to the live tail explicitly opens a fresh archive snapshot.
	if m.archivedLogs() && (m.logOffset > 0 || m.logSearch.result.Skip > 0) {
		return nil
	}
	if m.logSearch.path == m.snapshot.LogArchive && m.logSearch.query == m.logFilter &&
		m.logSearch.count == m.snapshot.LogCount {
		return nil
	}
	return m.searchArchive(0)
}

func (m *Model) pauseArchiveSearch() {
	if !m.logSearch.loading {
		return
	}
	if m.logSearch.cancel != nil {
		m.logSearch.cancel()
		m.logSearch.cancel = nil
	}
	m.logSearch.seq++
	m.logSearch.loading, m.logSearch.top = false, false
}

func (m *Model) archiveSearchResult(msg archiveSearchMsg) {
	if msg.seq != m.logSearch.seq {
		return
	}
	m.logSearch.loading = false
	m.logSearch.cancel = nil
	if msg.err != nil {
		m.bad, m.notice = true, "Log search failed: "+msg.err.Error()
		m.logSearch.count = msg.count
		return
	}
	m.logSearch.count, m.logSearch.result, m.logSearch.ready = msg.count, msg.result, true
	maximum := max(0, len(m.logLines(m.logWidth()))-m.navigationRows())
	m.logOffset = min(m.logOffset, maximum)
	if m.logSearch.top {
		m.logOffset = maximum
	}
}

func (m *Model) archivedLogs() bool {
	return m.logSearch.ready && m.logSearch.path == m.snapshot.LogArchive &&
		m.logSearch.query == m.logFilter &&
		m.logFilter != ""
}

func (m *Model) navigateArchivedLogs(key string) (bool, tea.Cmd) {
	if m.page != Logs || !m.archivedLogs() {
		return false, nil
	}
	first := key == "home" || (key == "g" && m.pendingG)
	last := key == "G" || key == "end"
	if first || last {
		m.pendingG = false
		m.logOffset = 0
		skip := 0
		if first {
			skip = -1
		}
		var cmd tea.Cmd
		if first {
			cmd = m.searchArchivePage(
				session.LogSearchOptions{Skip: skip, Before: m.logSearch.result.Before},
				m.logSearch.count,
			)
		} else {
			cmd = m.searchArchive(skip)
		}
		m.logSearch.top = first
		if first {
			m.logOffset = max(0, len(m.logLines(m.logWidth()))-m.navigationRows())
		}
		return true, cmd
	}
	switch key {
	case "k", "up", "ctrl+u", "ctrl+b", "pgup", "j", "down", "ctrl+d", "ctrl+f", "pgdown":
		old := m.logOffset
		m.navigate(key)
		if old != m.logOffset {
			if m.logOffset > 0 || m.logSearch.result.Skip > 0 {
				m.pauseArchiveSearch()
			}
			return true, nil
		}
		older := key == "k" || key == "up" || key == "ctrl+u" || key == "ctrl+b" || key == "pgup"
		skip := m.logSearch.result.Skip
		if older && skip+len(m.logSearch.result.Lines) < m.logSearch.result.Matches {
			m.logOffset = 0
			return true, m.searchArchivePage(
				session.LogSearchOptions{
					Skip:   skip + session.LogSearchPageSize,
					Before: m.logSearch.result.Before,
				},
				m.logSearch.count,
			)
		}
		if !older && skip > 0 {
			cmd := m.searchArchivePage(
				session.LogSearchOptions{
					Skip:   max(0, skip-session.LogSearchPageSize),
					Before: m.logSearch.result.Before,
				},
				m.logSearch.count,
			)
			m.logSearch.top = true
			return true, cmd
		}
		return true, nil
	}
	return false, nil
}
