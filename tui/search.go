package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/session"
)

type listSearch struct {
	input          textinput.Model
	page           Page
	previous       string
	cursor, offset int
	skip           int
	before         int64
	count          uint64
	edited         bool
}

func (m *Model) startSearch() tea.Cmd {
	if m.page != Profiles && m.page != Logs {
		return nil
	}
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "Search name, engine or path"
	previous := m.profileFilter
	if m.page == Logs {
		input.Placeholder = "Search current session logs"
		previous = m.logFilter
	}
	input.CharLimit = 200
	input.SetWidth(max(12, m.width-8))
	input.SetValue(previous)
	m.search = &listSearch{
		input:    input,
		page:     m.page,
		previous: previous,
		cursor:   m.cursor,
		offset:   m.logOffset,
		skip:     m.logSearch.result.Skip,
		before:   m.logSearch.result.Before,
		count:    m.logSearch.count,
	}
	m.pendingG = false
	return m.search.input.Focus()
}

func (m *Model) updateSearch(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			m.search = nil
			if m.page == Logs {
				return m.refreshArchiveSearch()
			}
			return nil
		case "esc":
			options := session.LogSearchOptions{Skip: m.search.skip, Before: m.search.before}
			count := m.search.count
			if options.Before == 0 {
				count = m.snapshot.LogCount
			}
			changed := m.search.edited
			if m.search.page == Logs {
				m.logFilter, m.logOffset = m.search.previous, m.search.offset
			} else {
				m.profileFilter, m.cursor = m.search.previous, m.search.cursor
				m.ensureProfileCursor()
			}
			m.search = nil
			if m.page == Logs {
				if changed {
					return m.searchArchivePage(options, count)
				}
				return nil
			}
			return nil
		case "ctrl+u":
			m.search.input.SetValue("")
		}
	}
	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	if m.search.page == Logs {
		if value := m.search.input.Value(); value != m.logFilter {
			m.search.edited = true
			m.logFilter, m.logOffset = value, 0
			return tea.Batch(cmd, m.searchArchive(0))
		}
	} else {
		m.profileFilter = m.search.input.Value()
		m.ensureProfileCursor()
	}
	return cmd
}
