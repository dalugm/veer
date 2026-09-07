package tui

import (
	"context"
	"errors"
	"runtime"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/privilege"
)

type authorization struct {
	input     textinput.Model
	options   engine.Options
	windows   bool
	cancelled bool
}
type authorizationMsg struct {
	authorized bool
	err        error
}

func (m *Model) beginAuthorization(options engine.Options) tea.Cmd {
	input := textinput.New()
	input.Prompt = "Password  "
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.CharLimit = 1024
	input.SetWidth(max(8, min(32, m.width-22)))
	m.auth = &authorization{input: input, options: options, windows: runtime.GOOS == "windows"}
	if m.auth.windows {
		cmd := m.runStart(options)
		m.busyLabel = "Waiting for Windows authorization"
		m.notice = "Approve or cancel the Windows UAC dialog."
		return cmd
	}
	ctx, cancel := m.begin("Checking administrator authorization")
	check := m.checkAuthorization
	return m.workers.track(func() tea.Msg {
		defer cancel()
		ok, err := check(ctx)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return authorizationMsg{ok, err}
	})
}

func (m *Model) authorizationResult(msg authorizationMsg) tea.Cmd {
	if m.auth == nil {
		return nil
	}
	if m.auth.cancelled {
		msg.err = context.Canceled
	}
	m.busy = false
	m.cancelWork = nil
	if msg.authorized && msg.err == nil {
		options := m.auth.options
		m.auth = nil
		return m.runStart(options)
	}
	if errors.Is(msg.err, context.Canceled) {
		m.auth = nil
		m.bad = false
		m.notice = "Authorization cancelled."
		return nil
	}
	m.bad = msg.err != nil
	m.notice = "Administrator access is required for TUN."
	if msg.err != nil {
		m.notice = "Authorization failed. Retry, or press Ctrl+T for system authentication."
	}
	return m.auth.input.Focus()
}

func (m *Model) updateAuthorization(message tea.Msg) tea.Cmd {
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.auth.input.SetValue("")
			if m.busy {
				m.auth.cancelled = true
				if m.cancelWork != nil {
					m.cancelWork()
				}
				m.notice = "Cancelling authorization…"
			} else {
				m.auth = nil
				m.bad = false
				m.notice = "Authorization cancelled."
			}
			return nil
		case "enter":
			if !m.busy {
				return m.submitAuthorization()
			}
		case "ctrl+t":
			if !m.busy && !m.auth.windows {
				m.auth.input.SetValue("")
				m.busy = true
				m.busyLabel = "System authentication"
				return tea.ExecProcess(
					privilege.AuthenticateCommand(),
					func(err error) tea.Msg { return authorizationMsg{err == nil, err} },
				)
			}
		}
	}
	if m.busy {
		return nil
	}
	var cmd tea.Cmd
	m.auth.input, cmd = m.auth.input.Update(message)
	return cmd
}

func (m *Model) submitAuthorization() tea.Cmd {
	password := []byte(m.auth.input.Value())
	m.auth.input.SetValue("")
	ctx, cancel := m.begin("Verifying administrator password")
	authorize := m.authorize
	// Clear even when the job fence declines queued work during shutdown.
	tracked := m.workers.track(func() tea.Msg {
		err := authorize(ctx, password)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return authorizationMsg{err == nil, err}
	})
	return func() tea.Msg { defer cancel(); defer clear(password); return tracked() }
}

func (m *Model) authorizationView(w, h int) string {
	body := "TUN needs administrator authorization.\n\n"
	if m.auth.windows {
		body += "Approve or cancel the Windows UAC dialog.\nVeer remains open while Windows authorizes the helper."
	} else if m.busy {
		body += m.busyLabel + "…\n\nEsc cancel"
	} else {
		body += m.auth.input.View() + "\n\nEnter authorize · Esc cancel\nCtrl+T system prompt (Touch ID / MFA)"
	}
	body += "\n\n" + faint.Render(clip(safe(m.notice), max(1, min(w-2, 64)-4)))
	card := box("AUTHORIZE TUN", body, min(w-2, 64), min(h-2, 16))
	return card
}
