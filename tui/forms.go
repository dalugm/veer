// Package tui implements Veer screens, forms and asynchronous user actions.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/geofile"
	"github.com/dalugm/veer/settings"
)

type form struct {
	kind, title, description string
	labels                   []string
	inputs                   []textinput.Model
	focus                    int
	geoSource                int
	pathSeq                  uint64
	pathItems                []string
	pathIndex                int
	pathLoading              bool
	pathAccept               bool
}

func (m *Model) openForm(kind, title, description string, labels, values []string) {
	m.clearPathCompletion()
	f := &form{kind: kind, title: title, description: description, labels: labels}
	for i := range labels {
		in := textinput.New()
		in.Prompt = "  "
		in.CharLimit = 2048
		in.SetValue(values[i])
		f.inputs = append(f.inputs, in)
	}
	f.inputs[0].Focus()
	f.resize(m.width)
	m.form = f
	m.notice = "Tab / Enter next field · Ctrl+S submit · Esc cancel"
	m.bad = false
}

func (f *form) resize(width int) {
	for i := range f.inputs {
		f.inputs[i].SetWidth(max(12, min(width-16, 76)))
	}
}

func (m *Model) openImport() {
	m.openForm(
		"import",
		"Add a profile",
		"Use an existing Xray JSON config. Its file and relative assets stay in place.",
		[]string{"Profile name", "Xray config path"},
		[]string{"", ""},
	)
}

func (m *Model) openEdit() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	p := m.config.Profiles[m.cursor]
	m.openForm(
		"edit",
		"Edit profile",
		"Change the profile name or Xray JSON config path.",
		[]string{"Profile name", "Xray config path"},
		[]string{p.Name, p.Path},
	)
	return m.requestPathCompletion()
}

func (m *Model) openSettings() tea.Cmd {
	m.openForm(
		"settings",
		"Engine settings",
		"Xray runs as an external process. A blank Geo directory uses the config folder.",
		[]string{
			"Xray executable (name in PATH or full path)",
			"Geo assets directory (optional)",
			"TUN DNS mode: auto / custom / off",
			"Custom DNS servers, comma-separated",
			"macOS network service (blank = automatic)",
		},
		[]string{
			m.config.EnginePath,
			m.config.GeoDir,
			m.config.DNSMode,
			m.config.DNS,
			m.config.NetworkService,
		},
	)
	return m.requestPathCompletion()
}

func (m *Model) openGeo() tea.Cmd {
	dir := m.config.GeoDir
	if dir == "" {
		if p, ok := m.config.Active(); ok {
			dir = filepath.Dir(p.Path)
		}
	}
	m.openForm(
		"geo",
		"Update Geo assets",
		"Download geoip.dat and geosite.dat. Existing files are retained if the update fails.",
		[]string{"Destination directory", "Source · ←/→ or h/l to choose"},
		[]string{dir, "github"},
	)
	return m.requestPathCompletion()
}

func (m *Model) updateForm(message tea.Msg) tea.Cmd {
	f := m.form
	if key, ok := message.(tea.KeyPressMsg); ok {
		if key.String() == "esc" {
			if m.busy {
				if m.cancelWork != nil {
					m.cancelWork()
					m.notice = "Cancelling…"
				}
				return nil
			}
			m.clearPathCompletion()
			m.form = nil
			m.notice = "Cancelled."
			m.bad = false
			return nil
		}
		if m.busy {
			return nil
		}
		if handled, cmd := m.pathKey(key.String()); handled {
			return cmd
		}
		if f.geoSourceField() {
			switch key.String() {
			case "right", "down", "l", "j":
				f.geoSource = (f.geoSource + 1) % len(geoSources)
				return nil
			case "left", "up", "h", "k":
				f.geoSource = (f.geoSource + len(geoSources) - 1) % len(geoSources)
				return nil
			}
		}
		switch key.String() {
		case "ctrl+s":
			return m.submitForm()
		case "enter":
			if f.focus == len(f.inputs)-1 {
				return m.submitForm()
			}
			fallthrough
		case "tab":
			m.clearPathCompletion()
			f.inputs[f.focus].Blur()
			f.focus = (f.focus + 1) % len(f.inputs)
			return tea.Batch(f.inputs[f.focus].Focus(), m.requestPathCompletion())
		case "shift+tab":
			m.clearPathCompletion()
			f.inputs[f.focus].Blur()
			f.focus = (f.focus + len(f.inputs) - 1) % len(f.inputs)
			return tea.Batch(f.inputs[f.focus].Focus(), m.requestPathCompletion())
		}
	}
	if m.busy || f.geoSourceField() {
		return nil
	}
	before, pos := f.inputs[f.focus].Value(), f.inputs[f.focus].Position()
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(message)
	if f.pathField() &&
		(before != f.inputs[f.focus].Value() || pos != f.inputs[f.focus].Position()) {
		return tea.Batch(cmd, m.requestPathCompletion())
	}
	return cmd
}

func (m *Model) submitForm() tea.Cmd {
	f := m.form
	if f == nil || m.busy {
		return nil
	}
	m.clearPathCompletion()
	values := make([]string, len(f.inputs))
	for i := range values {
		values[i] = strings.TrimSpace(f.inputs[i].Value())
	}
	if f.kind == "geo" {
		values[1] = geoSources[f.geoSource].value
	}
	ctx, cancel := m.begin("Saving…")
	c := m.config
	c.Profiles = append([]settings.Profile(nil), c.Profiles...)
	path := m.path
	cursor := m.cursor
	kind := f.kind
	return m.workers.track(func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return actionMsg{err: err}
		}
		switch kind {
		case "import":
			if err := c.AddProfile(values[0], cleanPath(values[1])); err != nil {
				return actionMsg{err: err}
			}
		case "edit":
			if values[0] == "" {
				return actionMsg{err: fmt.Errorf("enter a profile name")}
			}
			path := cleanPath(values[1])
			if path == "" {
				return actionMsg{err: fmt.Errorf("enter an Xray config path")}
			}
			path, err := filepath.Abs(path)
			if err != nil {
				return actionMsg{err: err}
			}
			if _, err := engine.Inspect(path); err != nil {
				return actionMsg{err: err}
			}
			for i, profile := range c.Profiles {
				if i != cursor && profile.Path == path {
					return actionMsg{err: fmt.Errorf("this config is already in Profiles")}
				}
			}
			c.Profiles[cursor].Name = values[0]
			c.Profiles[cursor].Path = path
		case "settings":
			if values[0] == "" {
				return actionMsg{err: fmt.Errorf("enter an Xray executable")}
			}
			c.EnginePath = cleanPath(values[0])
			if strings.ContainsAny(c.EnginePath, `/\`) {
				p, err := filepath.Abs(c.EnginePath)
				if err != nil {
					return actionMsg{err: err}
				}
				c.EnginePath = p
			}
			c.DNSMode = strings.ToLower(values[2])
			c.DNS = values[3]
			c.NetworkService = values[4]
			if _, err := c.DNSServers(); err != nil {
				return actionMsg{err: err}
			}
			c.GeoDir = cleanPath(values[1])
			if c.GeoDir != "" {
				p, err := filepath.Abs(c.GeoDir)
				if err != nil {
					return actionMsg{err: err}
				}
				c.GeoDir = p
			}
		case "geo":
			if values[0] == "" {
				return actionMsg{err: fmt.Errorf("choose a destination directory")}
			}
			err := geofile.Update(ctx, values[1], cleanPath(values[0]))
			return actionMsg{
				err:       err,
				notice:    "Geo assets updated.",
				closeForm: err == nil,
				geoDir:    cleanPath(values[0]),
			}
		}
		err := settings.Save(path, c)
		return actionMsg{
			err:       err,
			config:    &c,
			notice:    "Saved.",
			closeForm: err == nil,
			info:      inspectSelected(c),
		}
	})
}

func cleanPath(s string) string {
	return settings.ExpandPath(strings.Trim(strings.TrimSpace(s), "\"'"))
}
