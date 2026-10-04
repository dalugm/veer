package sharelink

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestToVLESSLinkReadsRealityPassword(t *testing.T) {
	config := testConfig()
	link, err := config.ToVLESSLink("out-vless", "Stable Node")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("pbk"); got != "public-password" {
		t.Fatalf("pbk = %q", got)
	}
	if got := u.Fragment; got != "Stable Node" {
		t.Fatalf("fragment = %q", got)
	}
}

func TestToVLESSLinkPrefersRealityPublicKey(t *testing.T) {
	config := testConfig()
	config.Outbounds[0].StreamSettings.RealitySettings.PublicKey = "actual-public-key"
	link, err := config.ToVLESSLink("out-vless", "Node")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("pbk"); got != "actual-public-key" {
		t.Fatalf("pbk = %q, want actual-public-key", got)
	}
}

func TestToVLESSLinkRejectsMissingUser(t *testing.T) {
	config := testConfig()
	config.Outbounds[0].Settings.Vnext[0].Users = nil
	if _, err := config.ToVLESSLink("out-vless", "Node"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestToVLESSLinkIncludesTLSFlowFingerprintAndALPN(t *testing.T) {
	config := testConfig()
	stream := config.Outbounds[0].StreamSettings
	stream.Security = "tls"
	stream.RealitySettings = nil
	stream.TLSSettings = &TLSSettings{
		ServerName:  "example.com",
		Fingerprint: "chrome",
		ALPN:        []string{"h2", "http/1.1"},
	}

	link, err := config.ToVLESSLink("out-vless", "TLS Node")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"flow": "xtls-rprx-vision",
		"fp":   "chrome",
		"alpn": "h2,http/1.1",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestToVLESSLinkIncludesXHTTPSettings(t *testing.T) {
	config := testConfig()
	stream := config.Outbounds[0].StreamSettings
	stream.Network = "xhttp"
	stream.XHTTPSettings = &XHTTPSettings{
		Host:  "cdn.example.com",
		Path:  "/private",
		Mode:  "stream-up",
		Extra: json.RawMessage(`{"xPaddingBytes":"100-1000"}`),
	}

	link, err := config.ToVLESSLink("out-vless", "XHTTP Node")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"type":  "xhttp",
		"host":  "cdn.example.com",
		"path":  "/private",
		"mode":  "stream-up",
		"extra": `{"xPaddingBytes":"100-1000"}`,
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestToVLESSLinkRejectsUnsupportedTransport(t *testing.T) {
	config := testConfig()
	config.Outbounds[0].StreamSettings.Network = "unsupported"
	if _, err := config.ToVLESSLink("out-vless", "Node"); err == nil {
		t.Fatal("expected unsupported transport to fail")
	}
}
