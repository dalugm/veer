package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/settings"
)

func (m *Model) openSubscription(source settings.Subscription) {
	title := "Add subscription"
	if source.ID != "" {
		title = "Edit subscription"
	}
	if source.Template == "" {
		if m.hasFocusedProfile() {
			source.Template = m.config.Profiles[m.cursor].Path
		} else if p, ok := m.config.Active(); ok {
			source.Template = p.Path
		}
	}
	m.openForm(
		"subscription",
		title,
		"Import VLESS nodes using local DNS, routing and listeners. Ctrl+S downloads and saves.",
		[]string{
			"Subscription name",
			"Subscription URL (hidden)",
			"Local Xray JSON template path",
			"VLESS outbound tag (blank = only VLESS outbound)",
		},
		[]string{source.Name, source.URL, source.Template, source.OutboundTag},
	)
	m.form.subscriptionID = source.ID
	m.form.inputs[1].EchoMode = textinput.EchoPassword
	m.form.inputs[1].EchoCharacter = '•'
}

func (m *Model) submitSubscription() tea.Cmd {
	f := m.form
	source := settings.Subscription{
		ID:          f.subscriptionID,
		Name:        f.inputs[0].Value(),
		URL:         f.inputs[1].Value(),
		Template:    cleanPath(f.inputs[2].Value()),
		OutboundTag: f.inputs[3].Value(),
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
