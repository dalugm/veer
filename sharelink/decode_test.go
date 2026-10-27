package sharelink

import (
	"encoding/base64"
	"strings"
	"testing"
)

func feedLink(name string) string {
	return "vless://11111111-2222-4333-8444-555555555555@node.example:443?type=tcp&security=reality&encryption=none&flow=xtls-rprx-vision&fp=chrome&sni=example.com&sid=abcd&pbk=" + base64.RawURLEncoding.EncodeToString(
		make([]byte, 32),
	) + "#" + name
}

func TestDecodeSubscriptionFormatsAndRoundTrip(t *testing.T) {
	plain := feedLink("Home") + "\r\n" + feedLink("Work") + "\n"
	for _, data := range []string{plain, "\ufeff" + plain, base64.StdEncoding.EncodeToString([]byte(plain)), base64.RawURLEncoding.EncodeToString([]byte(plain))} {
		nodes, err := DecodeSubscription([]byte(data))
		if err != nil || len(nodes) != 2 {
			t.Fatalf("decode: %v nodes=%d", err, len(nodes))
		}
		if nodes[0].Name != "Home" ||
			nodes[0].Outbound.Settings.Vnext[0].Address != "node.example" {
			t.Fatal("lost node fields")
		}
		link, err := (&XrayConfig{Outbounds: []Outbound{nodes[0].Outbound}}).ToVLESSLink(
			"",
			nodes[0].Name,
		)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeVLESSLink(link)
		if err != nil || got.Outbound.StreamSettings.RealitySettings.ShortID != "abcd" {
			t.Fatal("round trip failed", err)
		}
	}
}

func TestDecodeRejectsBadFeedsWithoutLeakingCredentials(t *testing.T) {
	for _, text := range []string{"", "<html>secret-token</html>", "ss://secret-token@example.com", "vless://secret-token@%", feedLink("Same") + "\n" + feedLink("Same"), feedLink("Valid") + "\nss://secret-token", strings.Replace(feedLink("Valid"), "type=tcp", "type=unsupported", 1), strings.Replace(feedLink("Valid"), "type=tcp", "type=tcp&type=ws", 1), strings.Replace(feedLink("Valid"), "sid=abcd", "sid=abc", 1)} {
		_, err := DecodeSubscription([]byte(text))
		if err == nil {
			t.Fatal("accepted malformed or unsupported feed")
		}
		if strings.Contains(err.Error(), "secret-token") ||
			strings.Contains(err.Error(), "11111111-2222") {
			t.Fatal("error leaked credentials")
		}
	}
}

func TestDecodeTLSTransports(t *testing.T) {
	base := "vless://11111111-2222-4333-8444-555555555555@example.com:443?security=tls&sni=example.com&"
	for _, suffix := range []string{"type=ws&path=%2Fws&host=cdn.example", "type=grpc&serviceName=proxy", "type=xhttp&mode=auto&path=%2Fproxy"} {
		node, err := DecodeVLESSLink(base + suffix + "#Test")
		if err != nil || node.Outbound.StreamSettings.TLSSettings.ServerName != "example.com" {
			t.Fatal(err)
		}
	}
}
