package network

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestLinuxProxyAvailability(t *testing.T) {
	for _, tc := range []struct {
		name        string
		output      string
		err         error
		unavailable bool
	}{
		{"missing command", "", &exec.Error{Name: "gsettings", Err: exec.ErrNotFound}, true},
		{"missing schemas", "org.example.other", nil, true},
		{"permission denied", "", errors.New("permission denied"), false},
		{"cancelled", "", context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := prepareProxy(
				t.Context(),
				"linux",
				"127.0.0.1:10808",
				"",
				func(_ context.Context, name string, args ...string) (string, error) {
					calls++
					if name != "gsettings" || len(args) != 1 || args[0] != "list-schemas" {
						t.Fatal("unexpected settings access")
					}
					return tc.output, tc.err
				},
			)
			if err == nil || errors.Is(err, ErrProxyUnavailable) != tc.unavailable || calls != 1 {
				t.Fatalf("availability: %v (%d calls)", err, calls)
			}
		})
	}
}

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
	for _, want := range []string{"-setsocksfirewallproxy Wi-Fi 127.0.0.1 7890", "-setsocksfirewallproxy Wi-Fi 127.0.0.1 1080", "-setsocksfirewallproxystate Wi-Fi on", "-setsocksfirewallproxystate Wi-Fi off"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestProxyLinuxRestoresAllGNOMEValues(t *testing.T) {
	values := map[string]string{
		"org.gnome.system.proxy:mode":           "'auto'",
		"org.gnome.system.proxy:use-same-proxy": "true",
		"org.gnome.system.proxy.socks:host":     "'old'",
		"org.gnome.system.proxy.socks:port":     "1080",
		"org.gnome.system.proxy.http:host":      "'h'",
		"org.gnome.system.proxy.http:port":      "8080",
		"org.gnome.system.proxy.https:host":     "'s'",
		"org.gnome.system.proxy.https:port":     "8443",
		"org.gnome.system.proxy.ftp:host":       "'f'",
		"org.gnome.system.proxy:ignore-hosts":   "['localhost']",
	}
	original := maps.Clone(values)
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if name == "gsettings" && len(args) == 1 && args[0] == "list-schemas" {
			return "org.gnome.system.proxy org.gnome.system.proxy.http org.gnome.system.proxy.https org.gnome.system.proxy.ftp org.gnome.system.proxy.socks", nil
		}
		if name != "gsettings" || len(args) < 3 {
			return "", errors.New("unexpected command")
		}
		key := args[1] + ":" + args[2]
		value, exists := values[key]
		if !exists {
			return "", fmt.Errorf("unknown GNOME setting %s", key)
		}
		if args[0] == "get" {
			return value, nil
		}
		values[key] = args[3]
		return "", nil
	}
	change, err := prepareProxy(t.Context(), "linux", "127.0.0.1:7890", "", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"org.gnome.system.proxy:mode":           "'manual'",
		"org.gnome.system.proxy:use-same-proxy": "false",
		"org.gnome.system.proxy.socks:host":     "'127.0.0.1'",
		"org.gnome.system.proxy.socks:port":     "7890",
		"org.gnome.system.proxy.http:host":      "''",
		"org.gnome.system.proxy.https:host":     "''",
		"org.gnome.system.proxy.ftp:host":       "''",
	} {
		if values[key] != want {
			t.Fatalf("%s = %s, want %s", key, values[key], want)
		}
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(values, original) {
		t.Fatalf("restored settings: %v, want %v", values, original)
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

func TestWindowsProxyRestoresAbsentAndEmptyValues(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			values := map[string]registryValue{
				"ProxyOverride": {kind: "REG_SZ", data: "localhost;*.example"},
			}
			if empty {
				values["ProxyServer"] = registryValue{kind: "REG_SZ"}
				values["ProxyEnable"] = registryValue{kind: "REG_DWORD", data: "0x0"}
			}
			original := maps.Clone(values)
			run := func(_ context.Context, name string, args ...string) (string, error) {
				if name != "reg" {
					return "", fmt.Errorf("unexpected command %s", name)
				}
				switch args[0] {
				case "query":
					var output strings.Builder
					for key, value := range values {
						fmt.Fprintf(&output, "%s    %s    %s\n", key, value.kind, value.data)
					}
					return output.String(), nil
				case "add":
					values[args[3]] = registryValue{kind: args[5], data: args[7]}
				case "delete":
					if _, exists := values[args[3]]; !exists {
						return "", errors.New("value missing")
					}
					delete(values, args[3])
				default:
					return "", errors.New("unexpected registry operation")
				}
				return "", nil
			}
			change, err := prepareProxy(t.Context(), "windows", "127.0.0.1:7890", "", run)
			if err != nil {
				t.Fatal(err)
			}
			notifications := 0
			change.notify = func(context.Context) error { notifications++; return nil }
			if err := change.Apply(t.Context()); err != nil {
				t.Fatal(err)
			}
			if values["ProxyServer"].data != "socks=127.0.0.1:7890" {
				t.Fatalf("proxy not applied: %v", values)
			}
			if err := change.Restore(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values, original) || notifications != 2 {
				t.Fatalf("restored %v, want %v; notifications %d", values, original, notifications)
			}
		})
	}
}
