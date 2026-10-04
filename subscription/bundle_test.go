package subscription

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
	if _, err := generate(t.Context(), manifestPath, output); err != nil {
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
		if containsStringForTest(rule["inboundTag"], "in-tun") {
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

func TestSafeOutputPathRejectsBroadDirectory(t *testing.T) {
	if _, err := safeOutputPath(t.TempDir()); err == nil {
		t.Fatal("expected broad output directory to be rejected")
	}
}
