package network

import (
	"context"
	"strings"
	"testing"
)

func TestProxyDarwinSnapshotsAndRestores(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if name == "networksetup" && args[0] == "-getsocksfirewallproxy" {
			return "Enabled: No\nServer: 127.0.0.1\nPort: 1080\n", nil
		}
		return "", nil
	}
	change, err := prepareProxy(t.Context(), "darwin", "127.0.0.1:7890", "Wi-Fi", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, "\n")
	for _, want := range []string{"-setsocksfirewallproxy Wi-Fi 127.0.0.1 7890", "-setsocksfirewallproxystate Wi-Fi on", "-setsocksfirewallproxystate Wi-Fi off"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestProxyLinuxRestoresAllGNOMEValues(t *testing.T) {
	values := map[string]string{
		"mode":         "'auto'",
		"socks host":   "'old'",
		"socks port":   "1080",
		"http host":    "'h'",
		"http port":    "8080",
		"https host":   "'s'",
		"https port":   "8443",
		"ignore-hosts": "['localhost']",
	}
	var calls []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "gsettings" && args[0] == "get" {
			return values[args[2]], nil
		}
		return "", nil
	}
	change, err := prepareProxy(t.Context(), "linux", "127.0.0.1:7890", "", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, "\n")
	if !strings.Contains(got, "set org.gnome.system.proxy mode 'manual'") ||
		!strings.Contains(got, "set org.gnome.system.proxy mode 'auto'") {
		t.Fatalf("proxy mode was not changed/restored: %s", got)
	}
}

func TestProxyWindowsRestoresRegistryValues(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "reg" && args[0] == "query" {
			return "ProxyEnable    REG_DWORD    0x0\nProxyServer    REG_SZ    http=old:80\n", nil
		}
		return "", nil
	}
	change, err := prepareProxy(t.Context(), "windows", "127.0.0.1:7890", "", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, "\n")
	if !strings.Contains(got, "ProxyServer /t REG_SZ /d socks=127.0.0.1:7890") ||
		!strings.Contains(got, "ProxyServer /t REG_SZ /d http=old:80") {
		t.Fatalf("proxy registry was not changed/restored: %s", got)
	}
}
