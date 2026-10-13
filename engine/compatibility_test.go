package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFeatureVersionRequirements(t *testing.T) {
	for _, tt := range []struct {
		name              string
		info              Info
		platform, version string
		bad               bool
	}{
		{"ordinary old core", Info{}, "linux", "Xray 1.8.0", false},
		{"old Lua DNS", Info{LuaDNS: true}, "darwin", "Xray 26.9.30", true},
		{"old Lua routing", Info{LuaRouting: true}, "windows", "Xray 26.9.30", true},
		{"current Lua", Info{LuaRouting: true}, "linux", "Xray 26.10.10 (Xray, Penetrates Everything.) 701af60", false},
		{"future Lua", Info{LuaDNS: true}, "darwin", "Xray 26.11.1", false},
		{"unknown custom core", Info{LuaDNS: true}, "darwin", "custom build", true},
		{"old Linux DNS", Info{TUN: true, TUNSystemDNS: true}, "linux", "Xray 26.9.1", true},
		{"current Linux DNS", Info{TUN: true, TUNSystemDNS: true}, "linux", "Xray 26.9.30", false},
		{"ignored macOS DNS flag", Info{TUN: true, TUNSystemDNS: true}, "darwin", "Xray 26.9.1", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.info.CheckVersion(tt.version, tt.platform); (err != nil) != tt.bad {
				t.Fatalf("CheckVersion: %v", err)
			}
		})
	}
}

func TestInspectLuaScripts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"dns":{"script":"dns.lua"},"routing":{"script":"routing.lua"},"inbounds":[{"protocol":"tun","settings":{"autoSystemRoutingTable":["0.0.0.0/0","::/0"]}}],"outbounds":[{"protocol":"freedom"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(path)
	if err != nil || !info.LuaDNS || !info.LuaRouting {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}
