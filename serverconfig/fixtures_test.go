package serverconfig

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dalugm/veer/sharelink"
)

func testConfig() *sharelink.XrayConfig {
	return &sharelink.XrayConfig{Outbounds: []sharelink.Outbound{{
		Protocol: "vless",
		Tag:      "out-vless",
		Settings: sharelink.Settings{Vnext: []sharelink.Vnext{{
			Address: "203.0.113.10",
			Port:    443,
			Users: []sharelink.User{{
				ID:         "4521497c-0eac-41f3-8746-1afcbacc205c",
				Encryption: "none",
				Flow:       "xtls-rprx-vision",
			}},
		}}},
		StreamSettings: &sharelink.StreamSettings{
			Network:  "raw",
			Security: "reality",
			RealitySettings: &sharelink.RealitySettings{
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
