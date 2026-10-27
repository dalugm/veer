package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/settings"
)

var subscriptionModes = [...]struct{ value, label string }{
	{"mixed", "Mixed"},
	{"tun", "TUN + Mixed"},
	{"custom", "Existing JSON"},
}

func (m *Model) openSubscription(source settings.Subscription) {
	title := "Add subscription"
	if source.ID != "" {
		title = "Edit subscription"
	}
	m.openForm(
		"subscription",
		title,
		"Import VLESS nodes with default DNS and routing, or use your own JSON. Ctrl+S downloads and saves.",
		[]string{
			"Subscription name",
			"Subscription URL · Ctrl+R hide",
			"Configuration · ←/→ to choose",
		},
		[]string{source.Name, source.URL, ""},
	)
	m.form.subscriptionID = source.ID
	m.form.subscriptionTemplate = source.Template
	m.form.subscriptionTag = source.OutboundTag
	if source.Template == "" {
		if m.hasFocusedProfile() {
			m.form.subscriptionTemplate = m.config.Profiles[m.cursor].Path
		} else if p, ok := m.config.Active(); ok {
			m.form.subscriptionTemplate = p.Path
		}
	}
	for i, mode := range subscriptionModes {
		if mode.value == source.ConfigMode() {
			m.form.setSubscriptionMode(i, m.width)
			break
		}
	}
	m.form.inputs[1].EchoCharacter = '•'
}

func (f *form) subscriptionURLField() bool { return f.kind == "subscription" && f.focus == 1 }

func (f *form) toggleSubscriptionURL() {
	in := &f.inputs[1]
	if in.EchoMode == textinput.EchoPassword {
		in.EchoMode = textinput.EchoNormal
		f.labels[1] = "Subscription URL · Ctrl+R hide"
	} else {
		in.EchoMode = textinput.EchoPassword
		f.labels[1] = "Subscription URL · Ctrl+R show"
	}
}

func (f *form) subscriptionModeField() bool { return f.kind == "subscription" && f.focus == 2 }

func (f *form) setSubscriptionMode(index, width int) {
	if len(f.inputs) == 5 {
		f.subscriptionTemplate = f.inputs[3].Value()
		f.subscriptionTag = f.inputs[4].Value()
	}
	f.subscriptionMode = index
	f.inputs, f.labels = f.inputs[:3], f.labels[:3]
	if subscriptionModes[index].value == "custom" {
		f.labels = append(
			f.labels,
			"Existing Xray JSON path",
			"VLESS outbound tag (optional when unique)",
		)
		for _, value := range []string{f.subscriptionTemplate, f.subscriptionTag} {
			in := textinput.New()
			in.Prompt, in.CharLimit = "  ", 2048
			in.SetValue(value)
			f.inputs = append(f.inputs, in)
		}
	}
	f.resize(width)
}

func (f *form) subscriptionModeView() string {
	choices := make([]string, len(subscriptionModes))
	for i, mode := range subscriptionModes {
		label, style := "○ "+mode.label, faint
		if i == f.subscriptionMode {
			label, style = "● "+mode.label, accent.Bold(true)
		}
		choices[i] = style.Render(label)
	}
	return "  " + strings.Join(choices, "   ")
}

func (m *Model) submitSubscription() tea.Cmd {
	f := m.form
	source := settings.Subscription{
		ID:   f.subscriptionID,
		Name: f.inputs[0].Value(),
		URL:  f.inputs[1].Value(),
		Mode: subscriptionModes[f.subscriptionMode].value,
	}
	if source.Mode == "custom" {
		source.Template = cleanPath(f.inputs[3].Value())
		source.OutboundTag = f.inputs[4].Value()
	}
	return m.syncSubscription(source, true)
}

func (m *Model) refreshSubscription() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	source, ok := m.config.Subscription(m.config.Profiles[m.cursor].SubscriptionID)
	if !ok {
		m.bad, m.notice = true, "Choose a subscription profile to refresh. Press u to add one."
		return nil
	}
	return m.syncSubscription(source, false)
}

func (m *Model) syncSubscription(source settings.Subscription, closeForm bool) tea.Cmd {
	source.URL = strings.TrimSpace(source.URL)
	ctx, cancel := m.begin("Downloading subscription…")
	report := m.startDownload("Subscription")
	c, path, client := m.config, m.path, m.subscriptions
	protected := ""
	running := m.running()
	if running {
		protected = c.Selected
	}
	focus := ""
	if m.hasFocusedProfile() {
		focus = c.Profiles[m.cursor].ID
	}
	return m.workers.track(func() tea.Msg {
		defer cancel()
		next, err := client.Sync(ctx, path, c, source, protected, report)
		notice := "Subscription saved."
		if err == nil {
			count := 0
			for _, p := range next.Profiles {
				s, _ := next.Subscription(p.SubscriptionID)
				if s.URL == source.URL {
					count++
					if closeForm && count == 1 {
						focus = p.ID
					}
				}
			}
			notice = fmt.Sprintf("Subscription saved · %d nodes.", count)
			if running {
				notice += " Current connection retained; restart to apply changes."
			}
		}
		return actionMsg{
			err:       err,
			config:    &next,
			notice:    notice,
			closeForm: closeForm,
			focusID:   focus,
			info:      inspectSelected(next),
		}
	})
}

func withoutSubscription(c settings.Config, id string) settings.Config {
	c.Subscriptions = slices.DeleteFunc(
		slices.Clone(c.Subscriptions),
		func(s settings.Subscription) bool { return s.ID == id },
	)
	c.Profiles = slices.DeleteFunc(
		slices.Clone(c.Profiles),
		func(p settings.Profile) bool { return p.SubscriptionID == id },
	)
	return c
}
