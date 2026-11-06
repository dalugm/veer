package geofile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializePreservesExistingAssetsAndValidatesSources(t *testing.T) {
	source, bad, destination := t.TempDir(), t.TempDir(), t.TempDir()
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if err := os.WriteFile(filepath.Join(source, name), validGeoFile(name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, name), []byte("not geodata"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	existing := []byte("user-managed geoip")
	if err := os.WriteFile(filepath.Join(destination, "geoip.dat"), existing, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(t.Context(), destination, []string{bad, source}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"geoip.dat": existing, "geosite.dat": validGeoFile("geosite.dat")} {
		got, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: unexpected data or error %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(source, name)); err != nil {
			t.Fatal("source removed", err)
		}
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestInitializeNoSourcesOrCancellationDoesNotCreateDirectory(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		if cancelled {
			cancel()
		}
		destination := filepath.Join(t.TempDir(), "geo")
		err := Initialize(ctx, destination, []string{filepath.Join(t.TempDir(), "missing")})
		cancel()
		if cancelled && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
		if !cancelled && err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created empty asset directory")
		}
	}
}

func TestInitialPublicationDoesNotReplaceConcurrentAsset(t *testing.T) {
	target := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(target, []byte("concurrent update"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishInitialGeo(t.Context(), target, validGeoFile("geosite.dat")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "concurrent update" {
		t.Fatal("overwrote existing asset", err)
	}
}
