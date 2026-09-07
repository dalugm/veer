package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/settings"
)

func press(m *Model, key rune) { m.Update(tea.KeyPressMsg{Code: key, Text: string(key)}) }

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
