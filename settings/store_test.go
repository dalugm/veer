package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripAndMissingDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	c, err := Load(path)
	if err != nil || c.EnginePath != "xray" {
		t.Fatalf("defaults: %#v %v", c, err)
	}
	c.Profiles = []Profile{
		{ID: "one", Name: "Home", Engine: "xray", Path: filepath.Join(t.TempDir(), "config.json")},
	}
	c.Selected = "one"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Selected != "one" || len(got.Profiles) != 1 {
		t.Fatalf("roundtrip: %#v %v", got, err)
	}
}

func TestMalformedStoreIsNotSilentlyReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(`{"profiles":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected corrupted settings error")
	}
}

func TestImportRetainsAbsolutePathAndRejectsDuplicates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(
		"config.json",
		[]byte(
			`{"inbounds":[{"protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	c := Defaults()
	if err := c.AddProfile("Home", "config.json"); err != nil {
		t.Fatal(err)
	}
	if c.Profiles[0].Path != filepath.Join(dir, "config.json") || c.Selected == "" {
		t.Fatalf("bad profile: %#v", c)
	}
	if err := c.AddProfile("Again", "config.json"); err == nil {
		t.Fatal("duplicate should fail")
	}
	if err := c.AddProfile("", "config.json"); err == nil {
		t.Fatal("empty name should fail")
	}
}

func TestConfigDirectoryOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VEER_CONFIG_DIR", dir)
	got, err := DefaultPath()
	if err != nil || got != filepath.Join(dir, "settings.json") {
		t.Fatalf("path=%q err=%v", got, err)
	}
}

func TestDNSModesAndMigration(t *testing.T) {
	for _, tt := range []struct{ data, mode, want string }{
		{`{"version":1}`, "auto", "1.1.1.1,8.8.8.8"},
		{`{"version":1,"dns":"9.9.9.9"}`, "custom", "9.9.9.9"},
		{`{"version":1,"dns_mode":"off","dns":"9.9.9.9"}`, "off", ""},
		{`{"version":1,"dns_mode":"auto","dns":"9.9.9.9"}`, "auto", "1.1.1.1,8.8.8.8"},
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		servers, err := c.DNSServers()
		if err != nil {
			t.Fatal(err)
		}
		if c.DNSMode != tt.mode || strings.Join(servers, ",") != tt.want {
			t.Fatalf("config=%+v servers=%v", c, servers)
		}
	}
	if Defaults().DNSMode != "auto" {
		t.Fatal("new settings must use auto DNS")
	}
	for _, c := range []Config{{DNSMode: "custom"}, {DNSMode: "custom", DNS: "bad"}, {DNSMode: "unknown"}} {
		if _, err := c.DNSServers(); err == nil {
			t.Fatalf("invalid DNS accepted: %+v", c)
		}
	}
}
