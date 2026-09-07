//go:build !windows

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/engine"
)

func TestAuthorizationMasksClearsAndRetries(t *testing.T) {
	m := newTestModel(t)
	m.checkAuthorization = func(context.Context) (bool, error) { return false, nil }
	var passed []byte
	m.authorize = func(_ context.Context, password []byte) error {
		if string(password) != " secret " {
			t.Fatal("password was trimmed or changed")
		}
		passed = password
		return errors.New(" secret ") // Never render raw authentication errors.
	}
	cmd := m.beginAuthorization(engine.Options{})
	m.Update(cmd())
	if m.auth == nil || m.busy {
		t.Fatal("password prompt not ready")
	}
	m.Update(tea.PasteMsg{Content: " secret "})
	if strings.Contains(ansi.Strip(m.View().Content), "secret") {
		t.Fatal("password visible")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.auth.input.Value() != "" {
		t.Fatal("input not cleared on submit")
	}
	m.Update(cmd())
	for _, b := range passed {
		if b != 0 {
			t.Fatal("password buffer retained")
		}
	}
	if m.auth == nil || m.busy || !m.bad || strings.Contains(m.notice, "secret") {
		t.Fatal("retry or private error failed")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.auth != nil {
		t.Fatal("cancel retained password dialog")
	}
}

func TestCachedAuthorizationAndCancelRace(t *testing.T) {
	m := newTestModel(t)
	m.checkAuthorization = func(context.Context) (bool, error) { return true, nil }
	cmd := m.beginAuthorization(engine.Options{})
	_, start := m.Update(cmd())
	if m.auth != nil || start == nil {
		t.Fatal("cached credentials should skip password prompt")
	}
	m.busy = false
	cmd = m.beginAuthorization(engine.Options{})
	result := cmd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, start = m.Update(result)
	if start != nil || m.auth != nil || m.busy {
		t.Fatal("cancelled authorization started a session")
	}
}

func TestPasswordWorkClearsWhenShutdownRejectsJob(t *testing.T) {
	m := newTestModel(t)
	m.checkAuthorization = func(context.Context) (bool, error) { return false, nil }
	m.Update(m.beginAuthorization(engine.Options{})())
	m.auth.input.SetValue("private")
	cmd := m.submitAuthorization()
	m.Shutdown()
	cmd()
	if m.auth.input.Value() != "" {
		t.Fatal("input retained")
	}
}

func TestToolsHaveNoServerMutationAction(t *testing.T) {
	m := newTestModel(t)
	m.page = Tools
	press(m, 'u')
	if m.form != nil || strings.Contains(m.View().Content, "server user") {
		t.Fatal("server action remains")
	}
}

func TestPasswordSuccessStartsConnection(t *testing.T) {
	m := newTestModel(t)
	m.checkAuthorization = func(context.Context) (bool, error) { return false, nil }
	m.authorize = func(context.Context, []byte) error { return nil }
	m.Update(m.beginAuthorization(engine.Options{})())
	m.auth.input.SetValue("password")
	_, start := m.Update(m.submitAuthorization()())
	if start == nil || m.auth != nil || !m.busy {
		t.Fatal("successful password did not continue connection")
	}
}
