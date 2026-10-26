package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

func press(m *Model, key rune) { m.Update(tea.KeyPressMsg{Code: key, Text: string(key)}) }

type logStreamBackend struct {
	idleBackend
	snapshot session.Snapshot
}

func (b *logStreamBackend) Snapshot() session.Snapshot { return b.snapshot }

func newLogStreamModel(t *testing.T, lines []string) (*Model, *logStreamBackend) {
	t.Helper()
	backend := &logStreamBackend{snapshot: session.Snapshot{
		State:    session.Running,
		Logs:     lines,
		LogCount: uint64(len(lines)),
	}}
	m, err := New(t.Context(), filepath.Join(t.TempDir(), "settings.json"), backend)
	if err != nil {
		t.Fatal(err)
	}
	m.page = Logs
	return m, backend
}

func TestVimPagesAndProfileNavigation(t *testing.T) {
	m := newTestModel(t)
	press(m, 'h')
	if m.page != Settings {
		t.Fatal("h did not wrap to previous page")
	}
	press(m, 'l')
	press(m, 'l')
	if m.page != Profiles {
		t.Fatal("l did not select Profiles")
	}
	m.config.Profiles = make([]settings.Profile, 30)
	press(m, 'j')
	press(m, 'j')
	press(m, 'k')
	if m.cursor != 1 {
		t.Fatal("j/k did not move cursor")
	}
	press(m, 'G')
	if m.cursor != 29 {
		t.Fatal("G did not select last row")
	}
	press(m, 'g')
	if m.cursor != 29 {
		t.Fatal("single g jumped")
	}
	press(m, 'g')
	if m.cursor != 0 {
		t.Fatal("gg did not select first row")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.cursor != 7 {
		t.Fatalf("half page moved to %d", m.cursor)
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.cursor != 0 {
		t.Fatal("ctrl+u did not return to top")
	}
}

func TestVimLogNavigation(t *testing.T) {
	m := newTestModel(t)
	m.page = Logs
	m.snapshot.Logs = make([]string, 100)
	press(m, 'g')
	press(m, 'g')
	if m.logOffset != 81 {
		t.Fatalf("gg should show a full first page, offset=%d", m.logOffset)
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.logOffset != 72 {
		t.Fatal("ctrl+d did not move half page down")
	}
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.logOffset != 53 {
		t.Fatal("ctrl+f did not move a full page down")
	}
	press(m, 'G')
	if m.logOffset != 0 {
		t.Fatal("G did not follow latest logs")
	}
	m.snapshot.Logs = nil
	press(m, 'g')
	press(m, 'g')
	press(m, 'k')
	if m.logOffset != 0 {
		t.Fatal("empty logs scrolled out of bounds")
	}
}

func TestScrolledLogsHoldStillWhileStreaming(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i)
	}
	m, backend := newLogStreamModel(t, lines)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tickMsg{})
	press(m, 'k')
	if m.logOffset != 1 {
		t.Fatalf("k did not scroll up, offset=%d", m.logOffset)
	}
	before := m.logBody(m.logWidth(), m.navigationRows()+1)
	for i := range 10 {
		backend.snapshot.Logs = append(backend.snapshot.Logs, fmt.Sprintf("streamed %02d", i))
	}
	backend.snapshot.LogCount += 10
	m.Update(tickMsg{})
	if m.logOffset != 11 {
		t.Fatalf("offset should grow with 10 streamed rows, got %d", m.logOffset)
	}
	if after := m.logBody(m.logWidth(), m.navigationRows()+1); after != before {
		t.Fatal("streaming logs moved the view the user is reading")
	}
	press(m, 'G')
	if m.logOffset != 0 {
		t.Fatal("G no longer returns to the live tail")
	}
}

func TestLiveLogsKeepFollowingTail(t *testing.T) {
	m, backend := newLogStreamModel(t, []string{"start"})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tickMsg{})
	backend.snapshot.Logs = append(backend.snapshot.Logs, "tail entry")
	backend.snapshot.LogCount++
	m.Update(tickMsg{})
	if m.logOffset != 0 {
		t.Fatal("live view should stay at the tail")
	}
	if !strings.Contains(m.logBody(m.logWidth(), m.navigationRows()+1), "tail entry") {
		t.Fatal("live view does not show the newest line")
	}
}

func TestScrolledLogsCountWrappedRows(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	m, backend := newLogStreamModel(t, lines)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tickMsg{})
	press(m, 'k')
	// At 80 columns the message column is 48 wide, so this line wraps to 3 rows.
	backend.snapshot.Logs = append(backend.snapshot.Logs, strings.Repeat("x", 100))
	backend.snapshot.LogCount++
	m.Update(tickMsg{})
	if m.logOffset != 4 {
		t.Fatalf("wrapped line should add 3 rows, got offset %d", m.logOffset)
	}
}

func TestVimPrefixCancelsAndFormsKeepText(t *testing.T) {
	m := newTestModel(t)
	m.page = Profiles
	m.config.Profiles = make([]settings.Profile, 10)
	m.cursor = 5
	press(m, 'g')
	press(m, 'j')
	press(m, 'g')
	if m.cursor != 6 {
		t.Fatal("interrupted gg still jumped")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	press(m, 'g')
	if m.cursor != 6 {
		t.Fatal("escape did not cancel gg")
	}
	m.openImport()
	for _, key := range "hjklggG" {
		press(m, key)
	}
	if got := m.form.inputs[0].Value(); got != "hjklggG" {
		t.Fatalf("navigation stole form text %q", got)
	}
	if m.page != Profiles || m.cursor != 6 {
		t.Fatal("form input changed navigation")
	}
}

func TestToolsKeepSingleKeyActions(t *testing.T) {
	m := newTestModel(t)
	m.page = Tools
	press(m, 'g')
	if m.form == nil || m.form.kind != "geo" {
		t.Fatal("g no longer opens Geo form")
	}
}
