package tui

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/geofile"
	"github.com/dalugm/veer/settings"
)

var geoSources = [...]struct{ value, label string }{
	{"github", "GitHub"},
	{"cdn", "jsDelivr"},
	{"fastly", "Fastly"},
}

func (f *form) geoSourceField() bool { return f.kind == "geo" && f.focus == 0 }

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
	files []geofile.FileInfo
}

func (m *Model) loadGeoInfo() tea.Cmd {
	m.geoSeq++
	seq := m.geoSeq
	dir := settings.GeoDirectory(m.path)
	m.geoFiles = nil
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		return geoInfoMsg{seq: seq, files: geofile.Inspect(ctx, dir)}
	}
}

func (m *Model) geoAssetsView(compact bool) string {
	lines := []string{faint.Render("Directory  " + safe(settings.GeoDirectory(m.path)))}
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
