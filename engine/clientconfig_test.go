package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientGenerationRejectsLuaTemplates(t *testing.T) {
	for _, section := range []string{"dns", "routing"} {
		data := []byte(`{"` + section + `":{"script":"private-script.lua"}}`)
		proxy, tun, err := BuildClientConfigs(data, "proxy", "short", "id")
		if err == nil || !strings.Contains(err.Error(), "templates with Lua") ||
			strings.Contains(err.Error(), "private-script") || proxy != nil || tun != nil {
			t.Fatalf("generation: %v", err)
		}
	}
}

func TestProxyVariantPreservesSharedInboundRules(t *testing.T) {
	for _, outbound := range []string{"out-block", "out-dns"} {
		t.Run(outbound, func(t *testing.T) {
			template := []byte(
				`{"inbounds":[{"tag":"in-mixed","protocol":"mixed"},{"tag":"in-tun","protocol":"tun"}],"outbounds":[{"tag":"out-vless","protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"old"}]}]},"streamSettings":{"realitySettings":{"shortId":"abcd"}}},{"tag":"out-dns","protocol":"dns"},{"tag":"out-block","protocol":"blackhole"}],"routing":{"rules":[{"inboundTag":["in-tun"],"port":53,"outboundTag":"out-dns"},{"inboundTag":["in-tun","in-mixed"],"outboundTag":"` + outbound + `"}]}}`,
			)
			proxy, tun, err := BuildClientConfigs(template, "out-vless", "abcd", "new")
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Inbounds  []struct{ Tag string }
				Outbounds []struct{ Tag string }
				Routing   struct {
					Rules []struct {
						InboundTag  []string
						OutboundTag string
					}
				}
			}
			if err := json.Unmarshal(proxy, &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Inbounds) != 1 || doc.Inbounds[0].Tag != "in-mixed" ||
				len(doc.Routing.Rules) != 1 || doc.Routing.Rules[0].OutboundTag != outbound ||
				len(
					doc.Routing.Rules[0].InboundTag,
				) != 1 || doc.Routing.Rules[0].InboundTag[0] != "in-mixed" {
				t.Fatalf("proxy lost shared routing policy: %s", proxy)
			}
			hasDNS := false
			for _, out := range doc.Outbounds {
				hasDNS = hasDNS || out.Tag == "out-dns"
			}
			if hasDNS != (outbound == "out-dns") {
				t.Fatal("incorrect DNS outbound retention")
			}
			if err := json.Unmarshal(tun, &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Routing.Rules) != 2 || len(doc.Routing.Rules[1].InboundTag) != 2 {
				t.Fatal("TUN variant was modified while generating proxy config")
			}
		})
	}
}
