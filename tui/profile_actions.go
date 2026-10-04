package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/settings"
)

func (m *Model) selectProfile() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	if m.running() {
		m.bad = true
		m.notice = "Disconnect before selecting another profile."
		return nil
	}
	c := m.config
	c.Selected = c.Profiles[m.cursor].ID
	m.begin("Saving selection…")
	return m.save(c, "Profile selected.", false)
}

func (m *Model) removeProfile() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	c := m.config
	c.Profiles = append([]settings.Profile(nil), c.Profiles...)
	id := c.Profiles[m.cursor].ID
	c.Profiles = append(c.Profiles[:m.cursor], c.Profiles[m.cursor+1:]...)
	if c.Selected == id {
		c.Selected = ""
		if len(c.Profiles) > 0 {
			c.Selected = c.Profiles[0].ID
		}
	}
	m.cursor = max(0, min(m.cursor, len(c.Profiles)-1))
	m.begin("Removing profile…")
	return m.save(c, "Profile removed. Original config file retained.", false)
}

func (m *Model) save(c settings.Config, notice string, closeForm bool) tea.Cmd {
	path := m.path
	return m.workers.track(func() tea.Msg {
		err := settings.Save(path, c)
		return actionMsg{
			err:       err,
			notice:    notice,
			config:    &c,
			closeForm: closeForm,
			info:      inspectSelected(c),
		}
	})
}
