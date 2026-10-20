package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/session"
)

func TestQuitFailurePreservesRestorationRetry(t *testing.T) {
	for _, state := range []session.State{session.Failed, session.Stopping} {
		t.Run(string(state), func(t *testing.T) {
			m, b := restartModel(t)
			b.state, b.cleanupPending = state, state == session.Failed
			b.stopErr = errors.New("DNS restoration failed: denied")
			_, cmd := m.Update(m.quit()())
			if cmd != nil || m.quitting || !m.bad || !strings.Contains(m.notice, "Press s") {
				t.Fatalf("lost recovery: quitting=%v notice=%s", m.quitting, m.notice)
			}
			b.stopErr = nil
			runCommands(m, m.pageKey("s"))
			if m.running() || m.bad || b.stops != 2 {
				t.Fatalf("retry failed: stops=%d notice=%s", b.stops, m.notice)
			}
			// The work fence must permit a fresh connection after recovery.
			runCommands(m, m.connect())
			if b.starts != 1 || !m.running() {
				t.Fatal("new connection remained fenced after recovery")
			}
			_, cmd = m.Update(m.quit()())
			if cmd == nil {
				t.Fatal("successful cleanup did not quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("missing quit message")
			}
		})
	}
}

func TestQuitKeepsPollingUntilCleanupCompletes(t *testing.T) {
	m, b := restartModel(t)
	cmd := m.quit()
	_, poll := m.Update(tickMsg(time.Now()))
	if poll == nil {
		t.Fatal("quitting broke the polling chain needed after a failed quit")
	}
	b.state, b.cleanupPending = session.Failed, true
	b.stopErr = context.DeadlineExceeded
	m.Update(cmd())
	if m.quitting || !m.bad {
		t.Fatal("timeout disabled cleanup retry")
	}
}

func TestQuitAllowsStoppedFailureWithoutPendingCleanup(t *testing.T) {
	m, b := restartModel(t)
	b.state, b.stopErr = session.Failed, errors.New("core already exited")
	_, cmd := m.Update(m.quit()())
	if cmd == nil {
		t.Fatal("terminal could not exit after a core failure without pending cleanup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("missing quit message")
	}
}
