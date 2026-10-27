package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsDanglingOrDuplicateSubscriptionMetadata(t *testing.T) {
	root := t.TempDir()
	source := Subscription{
		ID:       "source",
		Name:     "Home",
		URL:      "https://example.com/secret-token",
		Template: filepath.Join(root, "template.json"),
	}
	p := Profile{
		ID:             "node",
		Name:           "Home",
		Engine:         "xray",
		Path:           filepath.Join(root, "node.json"),
		SubscriptionID: "source",
		NodeKey:        "key",
	}
	for _, sources := range [][]Subscription{nil, {source, source}} {
		c := Defaults()
		c.Subscriptions, c.Profiles = sources, []Profile{p}
		path := filepath.Join(root, "settings.json")
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || strings.Contains(err.Error(), "secret-token") {
			t.Fatal("invalid metadata accepted or exposed URL")
		}
	}
	for _, raw := range []string{"file:///secret-token", "https://user:secret-token@example.com", "http://%secret-token", "https://example.com/#secret-token"} {
		if err := ValidateSubscriptionURL(
			raw,
		); err == nil ||
			strings.Contains(err.Error(), "secret-token") {
			t.Fatal("invalid URL accepted or exposed")
		}
	}
	c := Defaults()
	c.Subscriptions, c.Profiles = []Subscription{source}, []Profile{p}
	path := filepath.Join(root, "settings.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("settings are not private")
	}
}
