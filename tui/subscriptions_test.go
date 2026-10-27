package tui

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

func TestSubscriptionImportAndRefreshKeepConnectionAndHideSecrets(t *testing.T) {
	var requests atomic.Int32
	var host atomic.Value
	host.Store("old.example")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		link := "vless://11111111-2222-4333-8444-555555555555@" + host.Load().(string) + ":443?type=tcp&security=tls&sni=example.com#Node"
		_, _ = fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte(link)))
	}))
	defer server.Close()
	m := newTestModel(t)
	m.page = Profiles
	template := filepath.Join(t.TempDir(), "template.json")
	if err := os.WriteFile(
		template,
		[]byte(
			`{"inbounds":[{"protocol":"mixed","port":10808}],"outbounds":[{"tag":"proxy","protocol":"vless","settings":{},"streamSettings":{"network":"tcp"}}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	press(m, 'u')
	if m.form == nil || m.form.kind != "subscription" {
		t.Fatal("u did not open subscription import")
	}
	m.form.inputs[0].SetValue("Private feed")
	m.form.inputs[1].SetValue(server.URL + "/secret-token")
	m.form.inputs[2].SetValue(template)
	if strings.Contains(m.View().Content, "secret-token") || requests.Load() != 0 {
		t.Fatal("form exposed URL or downloaded without submission")
	}
	cmd := m.submitForm()
	if requests.Load() != 0 || cmd == nil {
		t.Fatal("submission blocked Update with network I/O")
	}
	runCommands(m, cmd)
	if m.bad || m.form != nil || len(m.config.Profiles) != 1 || requests.Load() != 1 {
		t.Fatal("import failed", m.notice)
	}
	p := m.config.Profiles[0]
	oldConfig, _ := os.ReadFile(p.Path)
	b := &restartBackend{state: session.Running}
	m.backend = b
	host.Store("new.example")
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil || requests.Load() != 1 || m.downloads == nil {
		t.Fatal("refresh was not asynchronous with progress")
	}
	runCommands(m, cmd)
	if m.bad || b.stops != 0 || b.starts != 0 || b.state != session.Running ||
		m.config.Selected != p.ID ||
		m.config.Profiles[0].Path == p.Path ||
		!strings.Contains(m.notice, "restart") {
		t.Fatal("refresh interrupted connection or lost selection", m.notice)
	}
	retained, _ := os.ReadFile(p.Path)
	if string(oldConfig) != string(retained) {
		t.Fatal("refresh replaced the running config")
	}
	press(m, 'i')
	if strings.Contains(m.View().Content, "secret-token") {
		t.Fatal("details exposed subscription URL")
	}
	m.showDetails = false
	b.state = session.Stopped
	press(m, 'e')
	if m.form == nil || m.form.subscriptionID == "" || m.form.title != "Edit subscription" ||
		strings.Contains(m.View().Content, "secret-token") {
		t.Fatal("subscription edit exposed credentials or opened path editor")
	}
	m.form = nil
	press(m, 'd')
	if !strings.Contains(ansi.Strip(m.View().Content), "Remove this subscription?") {
		t.Fatal("deletion did not describe the group")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	runCommands(m, cmd)
	if len(m.config.Subscriptions) != 0 || len(m.config.Profiles) != 0 {
		t.Fatal("subscription deletion left dangling entries")
	}
	if _, err := settings.Load(m.path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatal("deletion removed retained config")
	}
}

func TestSubscriptionEscapeCancelsWithoutWriting(t *testing.T) {
	m := newTestModel(t)
	m.openSubscription(settings.Subscription{})
	cmd := m.submitForm()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	runCommands(m, cmd)
	if !m.bad || !strings.Contains(m.notice, "canceled") || len(m.config.Profiles) != 0 {
		t.Fatal("cancelled queued import persisted", m.notice)
	}
	if _, err := os.Stat(m.path); !os.IsNotExist(err) {
		t.Fatal("cancelled import wrote settings")
	}
}
