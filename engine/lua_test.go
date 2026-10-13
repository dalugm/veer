package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRuntimeLuaPathsRemainRelativeToProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"dns":{"script":"dns.lua","queryStrategy":"UseIP"},"routing":{"script":"routing.lua","domainStrategy":"IPIfNonMatch"},"inbounds":[{"protocol":"mixed","port":10808}],"outbounds":[{"protocol":"freedom"}]}`
	for name, contents := range map[string]string{"config.json": data, "dns.lua": "dns script", "routing.lua": "routing script"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (Xray{}).Prepare(Options{Binary: binary, Config: path})
	if err != nil {
		t.Fatal(err)
	}
	plan, cleanup, err := (Xray{}).PrepareRuntime(plan)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	if filepath.Dir(plan.Args[2]) == dir {
		t.Fatal("expected a relocated runtime config")
	}
	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^TestLuaPathsProcess$")
	cmd.Dir = plan.Dir
	cmd.Env = append(os.Environ(), "VEER_LUA_PATH_TEST="+plan.Args[2])
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("relative script lookup: %v: %s", err, out)
	}
}

func TestLuaPathsProcess(t *testing.T) {
	path := os.Getenv("VEER_LUA_PATH_TEST")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		DNS     struct{ Script, QueryStrategy string }
		Routing struct{ Script, DomainStrategy string }
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.DNS.QueryStrategy != "UseIP" || doc.Routing.DomainStrategy != "IPIfNonMatch" {
		t.Fatal("native settings changed")
	}
	for path, want := range map[string]string{doc.DNS.Script: "dns script", doc.Routing.Script: "routing script"} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != want {
			t.Fatalf("script %q: %v", path, err)
		}
	}
}
