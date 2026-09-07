package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeConfigPreservesOriginalAndMergesStats(t *testing.T) {
	for _, api := range []string{``, `,"api":{"tag":"existing-api","listen":"127.0.0.1:32123","services":["HandlerService"],"custom":9007199254740993}`} {
		t.Run(api, func(t *testing.T) {
			original := `{"inbounds":[{"tag":"veer-outbound-1","protocol":"socks","port":1080}],"outbounds":[{"protocol":"freedom"},{"tag":"veer-outbound-2","protocol":"blackhole"}],"policy":{"levels":{"0":{"handshake":8}},"system":{"statsInboundUplink":true}},"routing":{"rules":[{"outboundTag":"existing-api","inboundTag":["api"]}]},"unknown":9007199254740993` + api + `}`
			path := filepath.Join(t.TempDir(), "original.json")
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			binary, _ := os.Executable()
			plan, err := (Xray{}).Prepare(Options{Binary: binary, Config: path})
			if err != nil {
				t.Fatal(err)
			}
			before := append([]string(nil), plan.Args...)
			runtime, cleanup, err := (Xray{}).PrepareRuntime(plan)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := cleanup(); err != nil {
					t.Error(err)
				}
			}()
			if !reflect.DeepEqual(plan.Args, before) || runtime.Args[2] == path ||
				runtime.CheckArgs[3] != runtime.Args[2] ||
				runtime.Dir != plan.Dir ||
				!reflect.DeepEqual(runtime.Env, plan.Env) {
				t.Fatalf("plans: %#v %#v", plan, runtime)
			}
			host, _, err := net.SplitHostPort(runtime.StatsAddress)
			if err != nil || host != "127.0.0.1" {
				t.Fatalf("address %q %v", runtime.StatsAddress, err)
			}
			data, err := os.ReadFile(runtime.Args[2])
			if err != nil {
				t.Fatal(err)
			}
			fi, _ := os.Stat(runtime.Args[2])
			if fi.Mode().Perm() != 0o600 {
				t.Fatal(fi.Mode())
			}
			gotOriginal, _ := os.ReadFile(path)
			if string(gotOriginal) != original {
				t.Fatal("original changed")
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if string(doc["unknown"]) != "9007199254740993" {
				t.Fatal("number lost precision")
			}
			var policy struct {
				Levels map[string]struct{ Handshake int }
				System map[string]bool
			}
			_ = json.Unmarshal(doc["policy"], &policy)
			if policy.Levels["0"].Handshake != 8 || !policy.System["statsInboundUplink"] ||
				!policy.System["statsOutboundUplink"] ||
				!policy.System["statsOutboundDownlink"] {
				t.Fatalf("policy %s", doc["policy"])
			}
			var cfgAPI struct {
				Tag, Listen string
				Services    []string
				Custom      json.RawMessage
			}
			_ = json.Unmarshal(doc["api"], &cfgAPI)
			if cfgAPI.Listen != runtime.StatsAddress ||
				!strings.Contains(string(doc["api"]), "StatsService") {
				t.Fatalf("api %s", doc["api"])
			}
			if api != "" &&
				(cfgAPI.Tag != "existing-api" || len(cfgAPI.Services) != 2 || string(cfgAPI.Custom) != "9007199254740993") {
				t.Fatalf("existing api lost %s", doc["api"])
			}
			var out []struct{ Tag string }
			_ = json.Unmarshal(doc["outbounds"], &out)
			if out[0].Tag == "" || out[0].Tag == "veer-outbound-1" || out[0].Tag == out[1].Tag ||
				out[1].Tag != "veer-outbound-2" {
				t.Fatalf("tags %#v", out)
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(runtime.Args[2]); !os.IsNotExist(err) {
				t.Fatalf("temp exists: %v", err)
			}
		})
	}
}

func TestSampleTrafficSubprocess(t *testing.T) {
	binary, _ := os.Executable()
	t.Setenv("VEER_STATS_HELPER", "1")
	for _, tc := range []struct {
		name             string
		upload, download uint64
		bad              bool
	}{
		{"success", 9007199254740993, 23, false}, {"malformed", 0, 0, true}, {"error", 0, 0, true}, {"negative", 0, 0, true}, {"oversized", 0, 0, true}, {"timeout", 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timeout := 3 * time.Second
			if tc.name == "timeout" {
				timeout = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			result, err := SampleTraffic(ctx, binary, tc.name)
			if (err != nil) != tc.bad {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error %v", err)
			}
			if !tc.bad &&
				(result.Upload != tc.upload || result.Download != tc.download || result.At.IsZero()) {
				t.Fatalf("result %#v", result)
			}
		})
	}
}

func init() {
	if os.Getenv("VEER_STATS_HELPER") != "1" {
		return
	}
	if len(os.Args) != 8 || os.Args[1] != "api" || os.Args[2] != "statsquery" ||
		os.Args[4] != "-timeout" ||
		os.Args[5] != "2" ||
		os.Args[6] != "-pattern" ||
		os.Args[7] != "outbound>>>" {
		os.Exit(4)
	}
	switch strings.TrimPrefix(os.Args[3], "--server=") {
	case "success":
		fmt.Print(
			`{"stat":[{"name":"outbound>>>proxy>>>traffic>>>uplink","value":"9007199254740993"},{"name":"outbound>>>proxy>>>traffic>>>downlink","value":20},{"name":"outbound>>>direct>>>traffic>>>downlink","value":"3"},{"name":"inbound>>>socks>>>traffic>>>uplink","value":"999"}]}`,
		)
	case "malformed":
		fmt.Print("secret invalid json")
	case "error":
		fmt.Fprint(os.Stderr, "secret error")
		os.Exit(3)
	case "negative":
		fmt.Print(`{"stat":[{"name":"outbound>>>proxy>>>traffic>>>uplink","value":"-1"}]}`)
	case "oversized":
		fmt.Print(strings.Repeat("x", 2<<20))
	case "timeout":
		time.Sleep(10 * time.Second)
	default:
		os.Exit(4)
	}
	os.Exit(0)
}

func TestStatsConfigRejectsNullOutbound(t *testing.T) {
	if _, err := statsConfig([]byte(`{"outbounds":[null]}`), "127.0.0.1:1"); err == nil {
		t.Fatal("accepted null outbound")
	}
}

func TestRuntimeCleanupReportsRemovalFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(
		path,
		[]byte(`{"inbounds":[],"outbounds":[{"protocol":"freedom"}]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Args: []string{"run", "-c", path}, CheckArgs: []string{"run", "-test", "-c", path}}
	runtime, cleanup, err := (Xray{}).PrepareRuntime(plan)
	if err != nil {
		t.Fatal(err)
	}
	temporary := runtime.Args[2]
	if err := os.Remove(temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(temporary, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	if err := os.WriteFile(filepath.Join(temporary, "block"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err == nil {
		t.Fatal("cleanup silently ignored removal failure")
	}
}

func TestStatsConfigGeneratesNoncollidingAPITag(t *testing.T) {
	data, err := statsConfig(
		[]byte(
			`{"inbounds":[{"tag":"veer-api-1"}],"outbounds":[{"tag":"veer-api-2","protocol":"freedom"}]}`,
		),
		"127.0.0.1:1234",
	)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ API struct{ Tag string } }
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.API.Tag == "" || doc.API.Tag == "veer-api-1" || doc.API.Tag == "veer-api-2" {
		t.Fatalf("invalid API tag %q", doc.API.Tag)
	}
}

func TestRuntimePreservesExistingAPIListen(t *testing.T) {
	for _, address := range []string{"127.0.0.1:32123", "[::1]:32123", "0.0.0.0:32123", "192.0.2.1:32123"} {
		t.Run(address, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := `{"api":{"tag":"api","listen":"` + address + `","services":["HandlerService"]},"outbounds":[{"protocol":"freedom"}]}`
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			plan := Plan{
				Args:      []string{"run", "-c", path},
				CheckArgs: []string{"run", "-test", "-c", path},
			}
			runtime, cleanup, err := (Xray{}).PrepareRuntime(plan)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := cleanup(); err != nil {
					t.Error(err)
				}
			}()
			data, err := os.ReadFile(runtime.Args[2])
			if err != nil {
				t.Fatal(err)
			}
			var doc struct{ API struct{ Listen string } }
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.API.Listen != address {
				t.Fatalf("API address changed from %q to %q", address, doc.API.Listen)
			}
			if strings.HasPrefix(address, "127.") || strings.HasPrefix(address, "[::1]") {
				if runtime.StatsAddress != address {
					t.Fatalf("stats address %q", runtime.StatsAddress)
				}
			} else if runtime.StatsAddress != "" || runtime.Args[2] != path {
				t.Fatal("nonloopback API should run original config with traffic unavailable")
			}
		})
	}
}

func TestRuntimePreservesLegacyRoutedAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"api":{"tag":"legacy-api","services":["HandlerService"]},"routing":{"rules":[{"inboundTag":["api-in"],"outboundTag":"legacy-api"}]},"outbounds":[{"protocol":"freedom"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Args: []string{"run", "-c", path}, CheckArgs: []string{"run", "-test", "-c", path}}
	runtime, cleanup, err := (Xray{}).PrepareRuntime(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtime, plan) {
		t.Fatalf("legacy API plan changed: %#v", runtime)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatal("legacy config changed")
	}
}
