package serverconfig

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestAddUserKeepsServerAndBothClientVariantsConsistent(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.json")
	templatePath := filepath.Join(dir, "template.json")
	original := []byte(
		`{"inbounds":[{"tag":"server","protocol":"vless","settings":{"clients":[]},"streamSettings":{"realitySettings":{"shortIds":["fedcba9876543210"]}}}]}`,
	)
	if err := os.WriteFile(serverPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePath, testTemplate(t), 0o600); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(dir, "client")
	result, err := AddUser(serverPath, templatePath, "alice", outputDir)
	if err != nil {
		t.Fatal(err)
	}
	server := readGeneratedDocument(t, serverPath)
	id := nestedString(t, server, "inbounds", "0", "settings", "clients", "0", "id")
	if result.ID != id || result.ClientDirectory != outputDir ||
		result.BackupPath != serverPath+".bak" {
		t.Fatalf("result does not describe the persisted user: %+v", result)
	}
	if id == "" ||
		nestedString(t, server, "inbounds", "0", "settings", "clients", "0", "email") != "alice" {
		t.Fatal("server client credentials were not added")
	}
	for _, kind := range []string{"tun", "proxy"} {
		client := readGeneratedDocument(t, filepath.Join(outputDir, "config-"+kind+".json"))
		out := findTaggedObjectForTest(t, client, "outbounds", "out-vless")
		if nestedString(t, out, "settings", "vnext", "0", "users", "0", "id") != id ||
			nestedString(
				t,
				out,
				"streamSettings",
				"realitySettings",
				"shortId",
			) != "fedcba9876543210" {
			t.Fatalf("%s client credentials differ from the server", kind)
		}
		if hasTaggedObject(t, client, "inbounds", "in-tun") != (kind == "tun") ||
			hasTaggedObject(t, client, "outbounds", "out-dns") != (kind == "tun") {
			t.Fatalf("%s config has incorrect TUN/DNS entries", kind)
		}
	}
	link, err := os.ReadFile(filepath.Join(outputDir, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(string(bytes.TrimSpace(link)))
	if err != nil || u.User.Username() != id || u.Fragment != "alice" {
		t.Fatalf("share link does not match the generated client: %v", err)
	}
	backup, err := os.ReadFile(serverPath + ".bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("original server config was not backed up: %v", err)
	}
	if _, err := AddUser(serverPath, templatePath, "alice", outputDir); err == nil {
		t.Fatal("duplicate user was accepted")
	}
}
