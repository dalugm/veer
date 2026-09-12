package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	update "github.com/dalugm/veer/engine/coreupdate"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

type restartBackend struct {
	state             session.State
	starts, stops     int
	stopErr, startErr error
}

func (b *restartBackend) Snapshot() session.Snapshot { return session.Snapshot{State: b.state} }
func (b *restartBackend) Stop(context.Context) error {
	b.stops++
	if b.stopErr == nil {
		b.state = session.Stopped
	}
	return b.stopErr
}

func (b *restartBackend) Start(context.Context, engine.Options) error {
	b.starts++
	if b.startErr != nil {
		b.state = session.Failed
		return b.startErr
	}
	b.state = session.Running
	return nil
}

func restartModel(t *testing.T) (*Model, *restartBackend) {
	t.Helper()
	m := newTestModel(t)
	b := &restartBackend{state: session.Running}
	m.backend = b
	m.runningVersion, m.version = "Xray 1.0.0", "Xray 1.0.0"
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(
		path,
		[]byte(
			`{"inbounds":[{"protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	m.config.Profiles = []settings.Profile{{ID: "one", Engine: "xray", Path: path}}
	m.config.Selected = "one"
	m.updates.readVersion = func(context.Context, string) (string, error) { return "Xray 1.1.0", nil }
	return m, b
}

func TestUpdatePromptsAndKeepsRunningVersionUntilRestart(t *testing.T) {
	for _, immediately := range []bool{true, false} {
		m, b := restartModel(t)
		_, cmd := m.Update(
			updateInstalledMsg{
				result: update.Result{Version: "v1.1.0", VersionOutput: "Xray 1.1.0"},
			},
		)
		if cmd != nil || b.stops != 0 || m.confirmation != "restart-update" ||
			m.coreLabel() != "Xray 1.0.0" {
			t.Fatal("installation restarted or changed running version")
		}
		// A subsequent disk version probe must not change the running version.
		m.Update(actionMsg{version: "Xray 1.1.0"})
		if m.coreLabel() != "Xray 1.0.0" {
			t.Fatal("disk version replaced running version")
		}
		if !immediately {
			press(m, 'n')
			if b.stops != 0 || m.coreLabel() != "Xray 1.0.0" {
				t.Fatal("declining restarted the connection")
			}
			_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		} else {
			_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		runCommands(m, cmd)
		if b.stops != 1 || b.starts != 1 || m.coreLabel() != "Xray 1.1.0" || m.busy {
			t.Fatalf(
				"restart: stops=%d starts=%d version=%s notice=%s",
				b.stops,
				b.starts,
				m.coreLabel(),
				m.notice,
			)
		}
		if !strings.Contains(m.updates.status, "Active.") {
			t.Fatal("update status did not reflect successful restart")
		}
	}
}

func TestRestartFailureAndCancellationDoNotStartAnotherCore(t *testing.T) {
	for _, kind := range []string{"stop failure", "cancel", "start failure"} {
		m, b := restartModel(t)
		if kind == "stop failure" {
			b.stopErr = errors.New("stop failed")
		}
		if kind == "start failure" {
			b.startErr = errors.New("start failed")
		}
		cmd := m.restart()
		msg := cmd()
		if kind == "cancel" {
			m.cancelWork()
		}
		_, next := m.Update(msg)
		runCommands(m, next)
		if kind != "start failure" && b.starts != 0 {
			t.Fatal("started after cancellation or stop failure")
		}
		if kind == "stop failure" && m.coreLabel() != "Xray 1.0.0" {
			t.Fatal("failed stop lost running version")
		}
		if kind == "start failure" && (m.runningVersion != "" || !m.bad) {
			t.Fatal("failed new core shown as running")
		}
	}
}

func TestOverviewPreservesRunningPrereleaseVersion(t *testing.T) {
	m, _ := restartModel(t)
	m.runningVersion = "Xray 1.2.0 (Xray, Penetrates Everything.) v1.2.0-rc.2 (go1.27 darwin/arm64)"
	m.Update(
		updateInstalledMsg{result: update.Result{Version: "v1.2.0", VersionOutput: "Xray 1.2.0"}},
	)
	press(m, 'n')
	if m.coreLabel() != "Xray 1.2.0-rc.2" {
		t.Fatalf("running prerelease lost: %s", m.coreLabel())
	}
}
