package geofile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectExistingFilesUsesModificationTime(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(name, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	items := Inspect(context.Background(), dir)
	if items[0].Version != "" || !items[0].Modified.Equal(stamp) || items[0].Err != nil {
		t.Fatalf("existing file: %+v", items[0])
	}
	if !os.IsNotExist(items[1].Err) {
		t.Fatalf("missing file: %+v", items[1])
	}
}

func TestUpdateRecordsOnlyMatchingReleaseAndDetectsReplacement(t *testing.T) {
	dir := t.TempDir()
	body := bytes.Repeat([]byte("geo"), 1024)
	release := releaseInfo{Tag: "202609072354"}
	// Use the actual release API decoder, including a mismatched CDN asset.
	withHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" {
			return response(
				http.StatusOK,
				fmt.Appendf(
					nil,
					`{"tag_name":%q,"assets":[{"name":"geosite.dat","digest":"sha256:%x"},{"name":"geoip.dat","digest":"sha256:wrong"}]}`,
					release.Tag,
					sha256.Sum256(body),
				),
			), nil
		}
		return response(http.StatusOK, body), nil
	})
	if err := Update(context.Background(), "cdn", dir); err != nil {
		t.Fatal(err)
	}
	items := Inspect(context.Background(), dir)
	if items[0].Version != release.Tag || items[0].Updated.IsZero() || items[0].Source != "cdn" {
		t.Fatalf("release: %+v", items[0])
	}
	if items[1].Version != "" || items[1].Modified.IsZero() {
		t.Fatalf("mismatched mirror: %+v", items[1])
	}
	if err := os.WriteFile(
		filepath.Join(dir, "geosite.dat"),
		[]byte("replaced"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(context.Background(), dir)[0]; got.Version != "" || !got.Updated.IsZero() {
		t.Fatalf("stale metadata: %+v", got)
	}
}

func TestMetadataFailurePreservesOldAssets(t *testing.T) {
	dir := t.TempDir()
	writeOldGeoFiles(t, dir)
	if err := os.Mkdir(filepath.Join(dir, metadataName), 0o700); err != nil {
		t.Fatal(err)
	}
	withHTTPClient(t, func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusOK, bytes.Repeat([]byte("new"), 1024)), nil
	})
	if err := updateGeoFiles(
		context.Background(),
		[]string{"https://example.test/geosite.dat", "https://example.test/geoip.dat"},
		dir,
	); err == nil {
		t.Fatal("expected publication failure")
	}
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != "old-"+name {
			t.Fatalf("old file changed: %s %v", b, err)
		}
	}
}

func TestReleaseUnavailableStillDownloads(t *testing.T) {
	dir := t.TempDir()
	withHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" {
			return response(http.StatusForbidden, nil), nil
		}
		return response(http.StatusOK, bytes.Repeat([]byte("geo"), 1024)), nil
	})
	if err := Update(context.Background(), "github", dir); err != nil {
		t.Fatal(err)
	}
	if item := Inspect(context.Background(), dir)[0]; item.Version != "" ||
		item.Modified.IsZero() ||
		item.Updated.IsZero() {
		t.Fatalf("fallback: %+v", item)
	}
}
