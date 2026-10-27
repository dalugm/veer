package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/geofile"
	"github.com/dalugm/veer/settings"
)

func TestConnectionsUseSharedGeoDirectory(t *testing.T) {
	m := newTestModel(t)
	destination := settings.GeoDirectory(m.path)
	config := filepath.Join(t.TempDir(), "native.json")
	if err := os.WriteFile(
		config,
		[]byte(
			`{"inbounds":[{"protocol":"mixed","port":10808}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := m.config.AddProfile("Local", config); err != nil {
		t.Fatal(err)
	}
	result := m.connect()().(prepareMsg)
	if result.err != nil || result.options.GeoDir != destination {
		t.Fatal("startup uses a different Geo directory", result.err)
	}
}

func TestMissingDefaultGeoPointsToDownload(t *testing.T) {
	m := newTestModel(t)
	config := filepath.Join(t.TempDir(), "default.json")
	if err := os.WriteFile(
		config,
		[]byte(
			`{"inbounds":[{"protocol":"mixed","port":10808}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := m.config.AddProfile("Node", config); err != nil {
		t.Fatal(err)
	}
	m.config.Profiles[0].SubscriptionID = "source"
	m.config.Subscriptions = []settings.Subscription{
		{ID: "source", Name: "Feed", URL: "https://example.com/feed", Mode: "mixed"},
	}
	result := m.connect()().(prepareMsg)
	if result.err == nil || !strings.Contains(result.err.Error(), "press g") ||
		!strings.Contains(result.err.Error(), settings.GeoDirectory(m.path)) {
		t.Fatal("missing assets did not explain how to initialize them", result.err)
	}
	if _, err := os.Stat(settings.GeoDirectory(m.path)); !os.IsNotExist(err) {
		t.Fatal("connection downloaded or created assets automatically")
	}
}

func TestNativeXrayUsesSharedGeoAfterRuntimeRelocation(t *testing.T) {
	binary, source := os.Getenv("VEER_TEST_XRAY"), os.Getenv("VEER_TEST_XRAY_ASSETS")
	if binary == "" || source == "" {
		t.Skip("set VEER_TEST_XRAY and VEER_TEST_XRAY_ASSETS for native validation")
	}
	m := newTestModel(t)
	m.config.EnginePath = binary
	destination := settings.GeoDirectory(m.path)
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	// Populate the isolated test directory without adding a product import API.
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := engine.DefaultProfile(
		[]byte(
			`{"protocol":"vless","settings":{"vnext":[{"address":"node.example","port":443,"users":[{"id":"11111111-2222-4333-8444-555555555555","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.com"}}}`,
		),
		"tun",
	)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "default.json")
	if err := os.WriteFile(config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.config.AddProfile("Default", config); err != nil {
		t.Fatal(err)
	}
	prepared := m.connect()().(prepareMsg)
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	core := engine.Xray{}
	plan, err := core.Prepare(prepared.options)
	if err != nil {
		t.Fatal(err)
	}
	plan, cleanup, err := core.PrepareRuntime(plan)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	// Validate only: never start the TUN, listeners or system network integration.
	cmd := exec.CommandContext(t.Context(), plan.Binary, plan.CheckArgs...)
	cmd.Env, cmd.Dir = append(os.Environ(), plan.Env...), plan.Dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("relocated runtime: %v\n%s", err, output)
	}
}

func TestGeoSourceSelector(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	if len(m.form.inputs) != 1 || !m.form.geoSourceField() {
		t.Fatal("Geo update still exposes a destination directory")
	}
	press(m, 'l')
	press(m, 'l')
	if m.form.geoSource != 2 {
		t.Fatal("source did not change")
	}
	press(m, 'l')
	if m.form.geoSource != 0 {
		t.Fatal("source did not wrap")
	}
	press(m, 'h')
	if m.form.geoSource != 2 {
		t.Fatal("reverse source selection failed")
	}
	m.Update(tea.PasteMsg{Content: "invalid-source"})
	press(m, 'x')
	if m.form.geoSource != 2 || m.busy {
		t.Fatal("typing changed source or started download")
	}
	for _, tc := range []struct {
		size       [2]int
		footerWant string
	}{{size: [2]int{60, 18}, footerWant: "? help"}, {size: [2]int{80, 24}, footerWant: "? help"}, {size: [2]int{100, 32}, footerWant: "Enter download"}} {
		size := tc.size
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"GitHub", "jsDelivr", "● Fastly", tc.footerWant} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing choice or action %q\n%s", want, view)
			}
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form != nil {
		t.Fatal("cancel did not close Geo form")
	}
}

func TestGeoAssetsRefreshAndCompactView(t *testing.T) {
	m := newTestModel(t)
	dir := settings.GeoDirectory(m.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2024, 1, 2, 3, 4, 0, 0, time.Local)
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("old data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	if cmd == nil {
		t.Fatal("entering Tools must read assets asynchronously")
	}
	m.Update(cmd())
	for _, size := range [][2]int{{60, 18}, {100, 32}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"geosite.dat", "geoip.dat", "Modified 2024-01-02 03:04"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q: %s", want, view)
			}
		}
	}
	old := m.loadGeoInfo()()
	m.loadGeoInfo()
	m.Update(old)
	if m.geoFiles != nil {
		t.Fatal("stale read changed current assets")
	}
	_, cmd = m.Update(actionMsg{geoUpdated: true, notice: "Geo assets updated."})
	if cmd == nil {
		t.Fatal("download did not refresh the shared assets")
	}
	runCommands(m, cmd)
	if len(m.geoFiles) != 2 {
		t.Fatal("shared assets were not reloaded")
	}
}

func TestGeoAssetsVersionsFitSmallTerminal(t *testing.T) {
	m := newTestModel(t)
	m.page = Tools
	m.geoFiles = []geofile.FileInfo{
		{Name: "geosite.dat", Version: "202609072354", Updated: time.Now()},
		{Name: "geoip.dat", Version: "202609072354", Updated: time.Now()},
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"geosite.dat  202609072354", "geoip.dat  202609072354"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
}
