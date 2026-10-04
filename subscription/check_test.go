package subscription

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	var checked []map[string]any
	check := func(ctx context.Context, binary, path, assets string) error {
		if ctx != t.Context() || binary != "fake-xray" || assets != root {
			t.Fatal("incorrect engine validation arguments")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		checked = append(checked, doc)
		return nil
	}

	if err := validateGeneratedConfigsWithXray(
		t.Context(),
		manifest,
		root,
		"fake-xray",
		check,
	); err != nil {
		t.Fatal(err)
	}
	if len(checked) != 2 || hasTaggedObject(t, checked[0], "inbounds", "in-tun") ||
		!hasTaggedObject(t, checked[1], "inbounds", "in-tun") {
		t.Fatal("both client variants were not validated")
	}
}
