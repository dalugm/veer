package geofile

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUpdateGeoFilesPublishesCompleteSet(t *testing.T) {
	destination := t.TempDir()
	writeOldGeoFiles(t, destination)
	withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
		body := bytes.Repeat([]byte(filepath.Base(request.URL.Path)), 256)
		return response(http.StatusOK, body), nil
	})

	urls := []string{"https://example.test/geoip.dat", "https://example.test/geosite.dat"}
	if err := updateGeoFiles(context.Background(), urls, destination); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"geoip.dat", "geosite.dat"} {
		data, err := os.ReadFile(filepath.Join(destination, filename))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), filename) {
			t.Fatalf("%s was not updated", filename)
		}
		info, err := os.Stat(filepath.Join(destination, filename))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filename, got)
		}
	}
}

func TestUpdateGeoFilesPreservesCompleteOldSetOnFailure(t *testing.T) {
	destination := t.TempDir()
	writeOldGeoFiles(t, destination)
	withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
		if filepath.Base(request.URL.Path) == "geosite.dat" {
			return response(http.StatusBadGateway, []byte("upstream failed")), nil
		}
		return response(http.StatusOK, bytes.Repeat([]byte("new-geoip"), 256)), nil
	})

	urls := []string{"https://example.test/geoip.dat", "https://example.test/geosite.dat"}
	if err := updateGeoFiles(context.Background(), urls, destination); err == nil {
		t.Fatal("expected update to fail")
	}
	for _, filename := range []string{"geoip.dat", "geosite.dat"} {
		data, err := os.ReadFile(filepath.Join(destination, filename))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(data), "old-"+filename; got != want {
			t.Fatalf("%s = %q, want %q", filename, got, want)
		}
	}
}

func TestExtractFilenameRejectsTraversal(t *testing.T) {
	if _, err := extractFilenameFromURL("https://example.test/files/.."); err == nil {
		t.Fatal("expected traversal filename to be rejected")
	}
}

func writeOldGeoFiles(t *testing.T, destination string) {
	t.Helper()
	for _, filename := range []string{"geoip.dat", "geosite.dat"} {
		if err := os.WriteFile(
			filepath.Join(destination, filename),
			[]byte("old-"+filename),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func withHTTPClient(t *testing.T, transport roundTripFunc) {
	t.Helper()
	original := httpClient
	httpClient = &http.Client{Transport: transport}
	t.Cleanup(func() { httpClient = original })
}

func response(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
