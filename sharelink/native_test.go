package sharelink

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestShareNativeTransportSettings(t *testing.T) {
	for _, tc := range []struct {
		name, stream, network, host string
	}{
		{"legacy network", `{"network":"tcp","security":"tls"}`, "tcp", ""},
		{"method only", `{"method":"raw","security":"tls"}`, "raw", ""},
		{"method overrides network", `{"method":"raw","network":"ws","security":"tls","wsSettings":{"path":"/old"}}`, "raw", ""},
		{"websocket method", `{"method":"websocket","security":"tls","wsSettings":{"host":"routing.example","path":"/proxy"}}`, "ws", "routing.example"},
		{"independent host", `{"network":"ws","security":"tls","wsSettings":{"host":"routing.example","path":"/proxy"}}`, "ws", "routing.example"},
		{"host overrides legacy header", `{"network":"ws","security":"tls","wsSettings":{"host":"routing.example","headers":{"Host":"old.example"}}}`, "ws", "routing.example"},
		{"legacy host header", `{"network":"ws","security":"tls","wsSettings":{"headers":{"Host":"routing.example"}}}`, "ws", "routing.example"},
		{"case insensitive legacy header", `{"network":"ws","security":"tls","wsSettings":{"headers":{"host":"routing.example"}}}`, "ws", "routing.example"},
		{"xhttp method", `{"method":"splithttp","security":"tls","xhttpSettings":{"path":"/proxy"}}`, "xhttp", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"node.example","port":443,"users":[{"id":"11111111-2222-4333-8444-555555555555","encryption":"none"}]}]},"streamSettings":` + tc.stream + `}]}`
			var c XrayConfig
			if err := json.Unmarshal([]byte(data), &c); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(c)
			link, err := c.ToVLESSLink("", "Node")
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(link)
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Get("type") != tc.network || u.Query().Get("host") != tc.host {
				t.Fatalf(
					"wrong transport: type=%q host=%q",
					u.Query().Get("type"),
					u.Query().Get("host"),
				)
			}
			node, err := DecodeVLESSLink(link)
			if err != nil {
				t.Fatal(err)
			}
			if tc.host != "" && node.Outbound.StreamSettings.WSSettings.Host != tc.host {
				t.Fatal("round trip lost WebSocket host")
			}
			after, _ := json.Marshal(c)
			if string(before) != string(after) {
				t.Fatal("sharing changed the original configuration")
			}
		})
	}
}

func TestEmptyMethodDoesNotFallBackToNetwork(t *testing.T) {
	c := testConfig()
	empty := ""
	c.Outbounds[0].StreamSettings.Method = &empty
	if _, err := c.ToVLESSLink("", "Node"); err == nil {
		t.Fatal("an explicitly empty method must not select the legacy network")
	}
}
