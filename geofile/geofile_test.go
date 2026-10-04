package geofile

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/dalugm/veer/engine"
	"google.golang.org/protobuf/encoding/protowire"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUpdateGeoFilesPublishesCompleteSet(t *testing.T) {
	destination := t.TempDir()
	writeOldGeoFiles(t, destination)
	withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
		body := validGeoFile(filepath.Base(request.URL.Path))
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
		if !bytes.Equal(data, validGeoFile(filename)) {
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
		return response(http.StatusOK, validGeoFile("geoip.dat")), nil
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

func validGeoFile(name string) []byte {
	entry := protowire.AppendTag(nil, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "TEST")
	for range 120 {
		var record []byte
		if name == "geoip.dat" {
			record = protowire.AppendTag(nil, 1, protowire.BytesType)
			record = protowire.AppendBytes(record, []byte{192, 0, 2, 0})
			record = protowire.AppendTag(record, 2, protowire.VarintType)
			record = protowire.AppendVarint(record, 24)
		} else {
			record = protowire.AppendTag(nil, 1, protowire.VarintType)
			record = protowire.AppendVarint(record, 2)
			record = protowire.AppendTag(record, 2, protowire.BytesType)
			record = protowire.AppendString(record, "example.test")
		}
		entry = protowire.AppendTag(entry, 2, protowire.BytesType)
		entry = protowire.AppendBytes(entry, record)
	}
	return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), entry)
}

func TestUpdateRejectsInvalidDataWithoutReplacingOldFiles(t *testing.T) {
	for _, invalid := range [][]byte{bytes.Repeat([]byte("<html>proxy error</html>"), 100), validGeoFile("geosite.dat")[:1024]} {
		dir := t.TempDir()
		writeOldGeoFiles(t, dir)
		withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
			if filepath.Base(request.URL.Path) == "geosite.dat" {
				return response(http.StatusOK, invalid), nil
			}
			return response(http.StatusOK, validGeoFile("geoip.dat")), nil
		})
		if err := updateGeoFiles(
			t.Context(),
			[]string{"https://example.test/geoip.dat", "https://example.test/geosite.dat"},
			dir,
		); err == nil {
			t.Fatal("invalid data accepted")
		}
		for _, name := range []string{"geoip.dat", "geosite.dat"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(data) != "old-"+name {
				t.Fatalf("old %s changed: %s %v", name, data, err)
			}
		}
	}
}

func TestGeoFixturesMatchXrayFormat(t *testing.T) {
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		if err := engine.ValidateGeoData(t.Context(), name, validGeoFile(name)); err != nil {
			t.Fatal(err)
		}
	}
}
