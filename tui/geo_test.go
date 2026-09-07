package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/geofile"
)

func TestGeoSourceSelector(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	// Directory remains normal text input, including Vim navigation letters.
	m.form.inputs[0].SetValue("")
	for _, r := range "hjkl" {
		press(m, r)
	}
	if m.form.inputs[0].Value() != "hjkl" {
		t.Fatal("selector intercepted directory input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, 'l')
	press(m, 'l')
	if m.form.geoSource != 2 {
		t.Fatal("source did not change")
	}
	press(m, 'l')
	if m.form.geoSource != 0 {
		t.Fatal("source did not wrap")
	}
	press(m, 'h')
	if m.form.geoSource != 2 {
		t.Fatal("reverse source selection failed")
	}
	m.Update(tea.PasteMsg{Content: "invalid-source"})
	press(m, 'x')
	if m.form.geoSource != 2 || m.busy {
		t.Fatal("typing changed source or started download")
	}
	for _, size := range [][2]int{{60, 18}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"GitHub", "jsDelivr", "● Fastly", "Enter download"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing choice or action %q\n%s", want, view)
			}
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form != nil {
		t.Fatal("cancel did not close Geo form")
	}
}

func TestGeoAssetsRefreshAndCompactView(t *testing.T) {
	m := newTestModel(t)
	dir := t.TempDir()
	stamp := time.Date(2024, 1, 2, 3, 4, 0, 0, time.Local)
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("old data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	m.config.GeoDir = dir
	_, cmd := m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	if cmd == nil {
		t.Fatal("entering Tools must read assets asynchronously")
	}
	m.Update(cmd())
	for _, size := range [][2]int{{60, 18}, {100, 32}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"geosite.dat", "geoip.dat", "Modified 2024-01-02 03:04"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q: %s", want, view)
			}
		}
	}
	old := m.loadGeoInfo(dir)()
	other := t.TempDir()
	m.loadGeoInfo(other)
	m.Update(old)
	if m.geoFiles != nil || m.geoDir != other {
		t.Fatal("stale read changed current assets")
	}
	_, cmd = m.Update(actionMsg{geoDir: dir, notice: "Geo assets updated."})
	if cmd == nil || m.geoDir != dir {
		t.Fatal("download did not refresh its destination")
	}
}

func TestGeoAssetsVersionsFitSmallTerminal(t *testing.T) {
	m := newTestModel(t)
	m.page, m.geoDir = Tools, "/geo"
	m.geoFiles = []geofile.FileInfo{
		{Name: "geosite.dat", Version: "202609072354", Updated: time.Now()},
		{Name: "geoip.dat", Version: "202609072354", Updated: time.Now()},
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"geosite.dat  202609072354", "geoip.dat  202609072354"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
}
