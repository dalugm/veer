package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/geofile"
)

var geoSources = [...]struct{ value, label string }{
	{"github", "GitHub"},
	{"cdn", "jsDelivr"},
	{"fastly", "Fastly"},
}

func (f *form) geoSourceField() bool { return f.kind == "geo" && f.focus == 1 }

func (f *form) geoSourceView() string {
	choices := make([]string, len(geoSources))
	for i, source := range geoSources {
		label := "○ " + source.label
		style := faint
		if i == f.geoSource {
			label = "● " + source.label
			style = accent.Bold(true)
		}
		choices[i] = style.Render(label)
	}
	return "  " + strings.Join(choices, "   ")
}

type geoInfoMsg struct {
	seq   uint64
	dir   string
	files []geofile.FileInfo
}

func (m *Model) geoDirectory() string {
	if m.config.GeoDir != "" {
		return cleanPath(m.config.GeoDir)
	}
	if p, ok := m.config.Active(); ok {
		return filepath.Dir(p.Path)
	}
	return ""
}

func (m *Model) loadGeoInfo(dir string) tea.Cmd {
	m.geoSeq++
	seq := m.geoSeq
	m.geoDir, m.geoFiles = dir, nil
	if dir == "" {
		return nil
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		return geoInfoMsg{seq: seq, dir: dir, files: geofile.Inspect(ctx, dir)}
	}
}

func (m *Model) geoAssetsView(compact bool) string {
	if m.geoDir == "" {
		return faint.Render("Select a profile or set a Geo directory to inspect assets.")
	}
	lines := []string{faint.Render("Directory  " + safe(m.geoDir))}
	if m.geoFiles == nil {
		return strings.Join(append(lines, "Reading assets…"), "\n")
	}
	for _, file := range m.geoFiles {
		status := ""
		switch {
		case os.IsNotExist(file.Err):
			status = "Not installed"
		case file.Err != nil:
			status = "Unable to read"
		case file.Version != "":
			status = file.Version
		default:
			status = "Modified " + file.Modified.Local().Format("2006-01-02 15:04")
		}
		lines = append(lines, accent.Render(safe(file.Name))+"  "+status)
		if !compact && !file.Updated.IsZero() {
			lines = append(
				lines,
				faint.Render("  Updated "+file.Updated.Local().Format(time.DateTime)),
			)
		}
	}
	return strings.Join(lines, "\n")
}
