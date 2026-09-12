package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCoreUpdateChannelMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, data := range []string{`{"version":1}`, `{"version":1,"core_update_channel":""}`, `{"version":1,"core_update_channel":"unknown"}`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil || c.CoreUpdateChannel != "stable" {
			t.Fatalf("migration: %+v %v", c, err)
		}
		c.CoreUpdateChannel = "preview"
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		c, err = Load(path)
		if err != nil || c.CoreUpdateChannel != "preview" {
			t.Fatalf("persistence: %+v %v", c, err)
		}
	}
}
