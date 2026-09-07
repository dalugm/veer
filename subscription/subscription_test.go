package subscription

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

func TestGenerateCreatesSubscriptionAndClientVariants(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "client.template.json")
	if err := os.WriteFile(templatePath, testTemplate(t), 0o600); err != nil {
		t.Fatal(err)
	}

	token := strings.Repeat("ab", 32)
	userID := "9ee6f4ad-94be-4915-96f2-31da71ba7625"
	shortID := "fedcba9876543210"
	manifest := Manifest{
		ClientTemplate: "client.template.json",
		OutboundTag:    "out-vless",
		Reality:        ManifestReality{ShortID: shortID},
		Label:          "Stable Node",
		Users: []ManifestUser{{
			Name:  "alice",
			ID:    userID,
			Token: token,
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "subscriptions.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "dist", "subscriptions")
	if _, err := generate(manifestPath, output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"subscription.txt", "config-proxy.json", "config-tun.json"} {
		if _, err := os.Stat(filepath.Join(output, token, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	link, err := os.ReadFile(filepath.Join(output, token, "subscription.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(link), "pbk=public-password") {
		t.Fatalf("generated link does not contain REALITY password: %s", link)
	}
	parsedLink, err := url.Parse(strings.TrimSpace(string(link)))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsedLink.User.Username(); got != userID {
		t.Fatalf("link user id = %q, want %q", got, userID)
	}
	if got := parsedLink.Query().Get("sid"); got != shortID {
		t.Fatalf("link short id = %q, want %q", got, shortID)
	}

	proxy := readGeneratedDocument(t, filepath.Join(output, token, "config-proxy.json"))
	tun := readGeneratedDocument(t, filepath.Join(output, token, "config-tun.json"))
	if hasTaggedObject(t, proxy, "inbounds", "in-tun") {
		t.Fatal("proxy config still contains in-tun")
	}
	if hasTaggedObject(t, proxy, "outbounds", "out-dns") {
		t.Fatal("proxy config still contains out-dns")
	}
	proxyRouting := proxy["routing"].(map[string]any)
	for _, value := range proxyRouting["rules"].([]any) {
		rule := value.(map[string]any)
		if containsString(rule["inboundTag"], "in-tun") {
			t.Fatal("proxy config still contains a in-tun routing rule")
		}
	}
	if !hasTaggedObject(t, tun, "inbounds", "in-tun") {
		t.Fatal("TUN config does not contain in-tun")
	}
	if !hasTaggedObject(t, tun, "outbounds", "out-dns") {
		t.Fatal("TUN config does not contain out-dns")
	}
	if !reflect.DeepEqual(
		findTaggedObjectForTest(t, proxy, "outbounds", "out-vless"),
		findTaggedObjectForTest(t, tun, "outbounds", "out-vless"),
	) {
		t.Fatal("proxy and TUN configs have different VLESS outbounds")
	}
	for name, document := range map[string]map[string]any{"proxy": proxy, "tun": tun} {
		outbound := findTaggedObjectForTest(t, document, "outbounds", "out-vless")
		if got := nestedString(
			t,
			outbound,
			"settings",
			"vnext",
			"0",
			"users",
			"0",
			"id",
		); got != userID {
			t.Fatalf("%s config user id = %q, want %q", name, got, userID)
		}
		if got := nestedString(
			t,
			outbound,
			"streamSettings",
			"realitySettings",
			"shortId",
		); got != shortID {
			t.Fatalf("%s config short id = %q, want %q", name, got, shortID)
		}
	}
}

func TestValidateGeneratedConfigsWithXrayChecksBothVariants(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "client.template.json")
	if err := os.WriteFile(templatePath, testTemplate(t), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{
		ClientTemplate: "client.template.json",
		OutboundTag:    "out-vless",
		Reality:        ManifestReality{ShortID: "fedcba9876543210"},
		Label:          "Node",
		Users: []ManifestUser{{
			Name:  "alice",
			ID:    "9ee6f4ad-94be-4915-96f2-31da71ba7625",
			Token: strings.Repeat("ab", 32),
		}},
	}

	markerDir := t.TempDir()
	originalCommand := xrayCommand
	xrayCommand = func(_ string, args ...string) *exec.Cmd {
		helperArgs := []string{"-test.run=TestXrayCommandHelper", "--"}
		helperArgs = append(helperArgs, args...)
		command := exec.Command(os.Args[0], helperArgs...)
		command.Env = append(os.Environ(), "GO_WANT_XRAY_HELPER=1", "XRAY_MARKER_DIR="+markerDir)
		return command
	}
	defer func() { xrayCommand = originalCommand }()

	if err := validateGeneratedConfigsWithXray(manifest, root, "fake-xray"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(markerDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("Xray checks = %d, want 2", len(entries))
	}
}

func TestXrayCommandHelper(t *testing.T) {
	if os.Getenv("GO_WANT_XRAY_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || len(os.Args[separator+1:]) != 4 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	if args[0] != "run" || args[1] != "-dump" || args[2] != "-config" {
		os.Exit(3)
	}
	data, err := os.ReadFile(args[3])
	if err != nil || !json.Valid(data) {
		os.Exit(4)
	}
	marker := filepath.Join(os.Getenv("XRAY_MARKER_DIR"), filepath.Base(args[3]))
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func TestSafeOutputPathRejectsBroadDirectory(t *testing.T) {
	if _, err := safeOutputPath(t.TempDir()); err == nil {
		t.Fatal("expected broad output directory to be rejected")
	}
}

func testConfig() *XrayConfig {
	return &XrayConfig{Outbounds: []Outbound{{
		Protocol: "vless",
		Tag:      "out-vless",
		Settings: Settings{Vnext: []Vnext{{
			Address: "203.0.113.10",
			Port:    443,
			Users: []User{{
				ID:         "4521497c-0eac-41f3-8746-1afcbacc205c",
				Encryption: "none",
				Flow:       "xtls-rprx-vision",
			}},
		}}},
		StreamSettings: &StreamSettings{
			Network:  "raw",
			Security: "reality",
			RealitySettings: &RealitySettings{
				ServerName:  "example.com",
				Password:    "public-password",
				ShortID:     "0123456789abcdef",
				Fingerprint: "chrome",
			},
		},
	}}}
}

func testTemplate(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["inbounds"] = []any{
		map[string]any{"tag": "in-socks", "protocol": "socks"},
		map[string]any{"tag": "in-tun", "protocol": "tun"},
	}
	document["outbounds"] = append(
		document["outbounds"].([]any),
		map[string]any{"tag": "out-dns", "protocol": "dns"},
	)
	document["routing"] = map[string]any{"rules": []any{
		map[string]any{"inboundTag": []any{"in-tun"}, "outboundTag": "out-dns"},
		map[string]any{"outboundTag": "out-vless"},
	}}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readGeneratedDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func hasTaggedObject(t *testing.T, document map[string]any, field, tag string) bool {
	t.Helper()
	items, ok := document[field].([]any)
	if !ok {
		t.Fatalf("%s is not an array", field)
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s entry is not an object", field)
		}
		if item["tag"] == tag {
			return true
		}
	}
	return false
}

func findTaggedObjectForTest(
	t *testing.T,
	document map[string]any,
	field, tag string,
) map[string]any {
	t.Helper()
	items, ok := document[field].([]any)
	if !ok {
		t.Fatalf("%s is not an array", field)
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s entry is not an object", field)
		}
		if item["tag"] == tag {
			return item
		}
	}
	t.Fatalf("%s has no object tagged %q", field, tag)
	return nil
}

func nestedString(t *testing.T, value any, path ...string) string {
	t.Helper()
	current := value
	for _, segment := range path {
		if segment == "0" {
			items, ok := current.([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("path %v has no first array entry", path)
			}
			current = items[0]
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %v encounters a non-object at %q", path, segment)
		}
		current = object[segment]
	}
	text, ok := current.(string)
	if !ok {
		t.Fatalf("path %v is not a string", path)
	}
	return text
}
