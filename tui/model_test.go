package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

type idleBackend struct{}

func (idleBackend) Start(context.Context, engine.Options) error { return nil }
func (idleBackend) Stop(context.Context) error                  { return nil }

func (idleBackend) Snapshot() session.Snapshot { return session.Snapshot{State: session.Stopped} }

func newTestModel(t *testing.T) *Model {
	t.Helper()
	m, err := New(t.Context(), filepath.Join(t.TempDir(), "settings.json"), idleBackend{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNavigationAndResize(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.page != Profiles {
		t.Fatal("profiles tab not selected")
	}
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 12}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View()
		if !v.AltScreen {
			t.Fatal("not full-screen")
		}
		for line := range strings.SplitSeq(v.Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("overflow %d: %q", size[0], line)
			}
		}
	}
}

func TestImportFormPersistsProfile(t *testing.T) {
	m := newTestModel(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(
		path,
		[]byte(
			`{"inbounds":[{"protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	m.openImport()
	m.form.inputs[0].SetValue("Home")
	m.form.inputs[1].SetValue(path)
	cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("no submit command")
	}
	m.Update(cmd())
	got, err := settings.Load(m.path)
	if err != nil || len(got.Profiles) != 1 || got.Profiles[0].Name != "Home" {
		t.Fatalf("import: %#v %v", got, err)
	}
	if m.form != nil {
		t.Fatal("successful form not closed")
	}
}

func TestEditProfileUpdatesNameAndConfigPath(t *testing.T) {
	const profileJSON = `{"inbounds":[{"protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"}]}`
	m := newTestModel(t)
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(profileJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	m.config.Profiles = []settings.Profile{{ID: "one", Name: "Old", Engine: "xray", Path: path}}
	m.cursor, m.page = 0, Profiles
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if cmd != nil || m.form == nil || m.form.kind != "edit" {
		t.Fatal("e did not open profile editor")
	}
	m.form.inputs[0].SetValue("New")
	if cmd := m.submitForm(); cmd == nil {
		t.Fatal("edit did not submit")
	} else {
		m.Update(cmd())
	}
	if len(m.config.Profiles) != 1 || m.config.Profiles[0].Name != "New" ||
		m.config.Profiles[0].Path != path {
		t.Fatalf("profile was not edited: %+v", m.config.Profiles)
	}
}

func TestInvalidImportKeepsForm(t *testing.T) {
	m := newTestModel(t)
	m.openImport()
	m.form.inputs[0].SetValue("Home")
	m.form.inputs[1].SetValue("/no/such/file")
	m.Update(m.submitForm()())
	if m.form == nil || m.notice == "" || len(m.config.Profiles) != 0 {
		t.Fatal("failed form lost or silently saved")
	}
}

func TestEscapeCancelsFormWithoutSaving(t *testing.T) {
	m := newTestModel(t)
	m.openImport()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form != nil {
		t.Fatal("form did not close")
	}
	if _, err := os.Stat(m.path); !os.IsNotExist(err) {
		t.Fatal("cancel wrote settings")
	}
}

func TestLastFormFieldVisibleAtMinimumSize(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.openSettings()
	for range 4 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.form.inputs[4].SetValue("output-visible")
	if !strings.Contains(ansi.Strip(m.View().Content), "output-visible") {
		t.Fatal("focused field is clipped below form")
	}
}

func TestOverviewShowsCoreWithoutRepeatingState(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m := newTestModel(t)
		m.version = "Xray 26.3.27 (Xray, Penetrates Everything.) Custom"
		m.snapshot = session.Snapshot{
			State: session.Running,
			Ready: true,
			PID:   42,
			Since: time.Now(),
		}
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		if strings.Count(view, "Connected") != 1 ||
			strings.Contains(strings.Split(view, "\n")[0], "Connected") {
			t.Fatalf("duplicate state at %v:\n%s", size, view)
		}
		if !strings.Contains(view, "Xray 26.3.27") {
			t.Fatalf("missing core at %v:\n%s", size, view)
		}
		if !strings.Contains(view, "TRAFFIC") || !strings.Contains(view, "? help") {
			t.Fatal(view)
		}
	}
}

func TestBackgroundVersionDoesNotInterruptSessionWork(t *testing.T) {
	m := newTestModel(t)
	m.busy, m.notice = true, "Connecting"
	m.Update(engineVersionMsg{binary: m.config.EnginePath, version: "Xray 26.3.27"})
	if m.version != "Xray 26.3.27" || !m.busy || m.notice != "Connecting" {
		t.Fatal("version probe changed operation state")
	}
	m.Update(engineVersionMsg{binary: "/old/core", version: "Xray old"})
	if m.version != "Xray 26.3.27" {
		t.Fatal("old core probe overwrote current version")
	}
}

func TestChangingCoreRefreshesVersion(t *testing.T) {
	m := newTestModel(t)
	m.version = "Xray old"
	c := m.config
	c.EnginePath = filepath.Join(t.TempDir(), "missing-core")
	_, cmd := m.Update(actionMsg{config: &c, notice: "Saved"})
	if cmd == nil || m.version != "Checking…" {
		t.Fatal("core change did not refresh version")
	}
	runCommands(m, cmd)
	if m.version != "Unavailable" || m.bad || m.notice != "Saved" {
		t.Fatal("failed automatic probe changed application notice")
	}
}

func TestConnectionStatusRequiresReadiness(t *testing.T) {
	m := newTestModel(t)
	m.snapshot.State = session.Running
	if strings.Contains(m.connectionStatus(), "Connected") {
		t.Fatal("unverified process advertised as connected")
	}
	m.snapshot.Ready = true
	m.page = Logs
	if !strings.Contains(strings.Split(ansi.Strip(m.View().Content), "\n")[0], "Connected") {
		t.Fatal("status missing on other pages")
	}
}
