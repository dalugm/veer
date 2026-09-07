package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectConfig(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		bad, tun   bool
	}{
		{"socks", `{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":1080}],"outbounds":[{"protocol":"vless"}]}`, false, false},
		{"tun", `{"inbounds":[{"protocol":"tun","settings":{"name":"veer0"}}],"outbounds":[{"protocol":"freedom"}]}`, false, true},
		{"null", `null`, true, false},
		{"empty", `{}`, true, false},
		{"trailing", `{} {}`, true, false},
		{"invalid", `{"inbounds":false}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := Inspect(p)
			if (err != nil) != tc.bad {
				t.Fatalf("%#v %v", info, err)
			}
			if err == nil && info.TUN != tc.tun {
				t.Fatalf("TUN=%v", info.TUN)
			}
		})
	}
}

func TestXrayCommandsUseArgumentsAndAssets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a b.json")
	if err := os.WriteFile(
		p,
		[]byte(
			`{"inbounds":[{"protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	plan, err := (Xray{}).Prepare(Options{Binary: exe, Config: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Args) != 3 || plan.Args[2] != p || plan.CheckArgs[1] != "-test" {
		t.Fatalf("bad plan: %#v", plan)
	}
}

func TestInspectTUNInterfaceName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tun.json")
	if err := os.WriteFile(
		path,
		[]byte(
			`{"inbounds":[{"protocol":"tun","settings":{"name":"veer-test"}}],"outbounds":[{"protocol":"freedom"}]}`,
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.TUNName != "veer-test" {
		t.Fatalf("TUN name %q", info.TUNName)
	}
}
