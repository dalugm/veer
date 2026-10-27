package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func presetNode(address string) []byte {
	return []byte(
		`{"protocol":"vless","settings":{"vnext":[{"address":"` + address + `","port":443,"users":[{"id":"11111111-2222-4333-8444-555555555555","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.com"}}}`,
	)
}

func TestDefaultProfilesKeepNodeTransportAndBootstrapIndependentOfProxy(t *testing.T) {
	for _, mode := range []string{"mixed", "tun"} {
		t.Run(mode, func(t *testing.T) {
			data, err := DefaultProfile(presetNode("new-node.example"), mode)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				DNS struct {
					QueryStrategy string
					Servers       []clientDNS
				}
				Routing   struct{ Rules []clientRule }
				Outbounds []struct {
					StreamSettings struct {
						Network     string
						TLSSettings struct{ ServerName string }
					}
				}
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			bootstrap := doc.DNS.Servers[0]
			if !slices.Equal(bootstrap.Domains, []string{"full:new-node.example"}) ||
				bootstrap.Address != clientDirectDNS || bootstrap.Tag != "dns-direct" ||
				!bootstrap.FinalQuery || !bootstrap.SkipFallback || len(bootstrap.ExpectedIPs) != 0 {
				t.Fatal(
					"node bootstrap can recurse through the proxy or reject a foreign node IP",
					bootstrap,
				)
			}
			if doc.Routing.Rules[0].OutboundTag != "direct" ||
				!slices.Contains(doc.Routing.Rules[0].InboundTag, bootstrap.Tag) {
				t.Fatal("bootstrap DNS does not have a direct route")
			}
			google, china := -1, -1
			for i, rule := range doc.Routing.Rules {
				if slices.Contains(rule.Domain, "geosite:google") {
					google = i
				}
				if slices.Contains(rule.Domain, "geosite:cn") {
					china = i
				}
				for _, domain := range rule.Domain {
					found := slices.ContainsFunc(doc.DNS.Servers, func(s clientDNS) bool {
						wantTag := "dns-" + rule.OutboundTag
						return slices.Contains(s.Domains, domain) && s.Tag == wantTag
					})
					if !found {
						t.Fatalf("routing and DNS disagree for %s", domain)
					}
				}
			}
			last := doc.Routing.Rules[len(doc.Routing.Rules)-1]
			if google < 0 || china <= google || last.OutboundTag != "proxy" ||
				last.Network != "tcp,udp" {
				t.Fatal("Google precedence or default proxy route lost")
			}
			if doc.DNS.QueryStrategy != "UseIPv4" ||
				doc.Outbounds[0].StreamSettings.Network != "tcp" ||
				doc.Outbounds[0].StreamSettings.TLSSettings.ServerName != "example.com" {
				t.Fatal("changed node transport or IPv4 DNS default")
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := Inspect(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.TUN != (mode == "tun") || info.ProxyEndpoint != "127.0.0.1:10808" ||
				(mode == "tun" && (!info.TUNIPv4 || !info.TUNIPv6 || !info.TUNSystemDNS)) {
				t.Fatal("wrong native listeners or capture", info)
			}
		})
	}
}

func TestDefaultProfileRebuildsBootstrapForEveryNode(t *testing.T) {
	for _, address := range []string{"one.example", "two.example", "192.0.2.1", "2001:db8::1"} {
		data, err := DefaultProfile(presetNode(address), "mixed")
		if err != nil {
			t.Fatal(err)
		}
		var doc struct{ DNS struct{ Servers []clientDNS } }
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		want := strings.HasSuffix(address, ".example")
		found := slices.ContainsFunc(doc.DNS.Servers, func(s clientDNS) bool {
			return slices.Contains(s.Domains, "full:"+address) && s.FinalQuery
		})
		if found != want {
			t.Fatalf("bootstrap for %s = %v", address, found)
		}
		if strings.Contains(string(data), "pautai.top") {
			t.Fatal("personal domain leaked into defaults")
		}
	}
}

func TestNativeXrayAcceptsDefaultProfiles(t *testing.T) {
	binary := os.Getenv("VEER_TEST_XRAY")
	assets := os.Getenv("VEER_TEST_XRAY_ASSETS")
	if binary == "" || assets == "" {
		t.Skip("set VEER_TEST_XRAY and VEER_TEST_XRAY_ASSETS for native validation")
	}
	for _, mode := range []string{"mixed", "tun"} {
		t.Run(mode, func(t *testing.T) {
			data, err := DefaultProfile(presetNode("node.example"), mode)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			// -test builds the configuration without starting listeners or changing routes/DNS.
			cmd := exec.CommandContext(t.Context(), binary, "run", "-test", "-c", path)
			cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assets)
			cmd.Dir = filepath.Dir(path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("native validation: %v\n%s", err, output)
			}
		})
	}
}
