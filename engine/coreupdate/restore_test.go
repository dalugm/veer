package coreupdate

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
)

func TestRestoreRetainsBothVersionsWithoutNetwork(t *testing.T) {
	c, release, target := installFixture(t)
	installed, err := c.Install(t.Context(), target, "v1.0.0", release)
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("restore must work offline")
		return nil, errors.New("offline")
	})
	c.version = func(_ context.Context, path string) (string, error) {
		data, err := os.ReadFile(path)
		if string(data) == "old executable" {
			return "Xray 1.0.0", err
		}
		return "Xray 1.1.0", err
	}
	restored, err := c.Restore(t.Context(), target, installed.BackupPath, installed.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != "v1.0.0" {
		t.Fatalf("restored %s", restored.Version)
	}
	assertContents(t, target, "old executable")
	assertContents(t, installed.BackupPath, "old executable")
	assertContents(t, restored.BackupPath, "new executable")
}

func TestRestoreFailurePreservesInstalledCore(t *testing.T) {
	for _, kind := range []string{"missing", "invalid version", "cancelled", "different target"} {
		t.Run(kind, func(t *testing.T) {
			c, release, target := installFixture(t)
			installed, err := c.Install(t.Context(), target, "v1.0.0", release)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "missing":
				installed.BackupPath += ".missing"
			case "invalid version":
				c.version = func(context.Context, string) (string, error) { return "unknown", nil }
			case "cancelled":
				cancel()
			case "different target":
				installed.TargetPath += ".another-installation"
			}
			if _, err := c.Restore(
				ctx,
				target,
				installed.BackupPath,
				installed.TargetPath,
			); err == nil {
				t.Fatal("restore should fail")
			}
			assertContents(t, target, "new executable")
		})
	}
}
