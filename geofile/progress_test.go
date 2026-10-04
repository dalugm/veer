package geofile

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dalugm/veer/download"
)

func TestGeoProgressReportsBothFiles(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known", false: "unknown"}[known], func(t *testing.T) {
			withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
				body := validGeoFile(filepath.Base(request.URL.Path))
				resp := response(http.StatusOK, body)
				resp.ContentLength = -1
				if known {
					resp.ContentLength = int64(len(body))
				}
				return resp, nil
			})
			var mu sync.Mutex
			latest := make(map[string]download.Progress)
			err := updateGeoFilesWithMetadata(t.Context(), []string{
				"https://example.test/geoip.dat", "https://example.test/geosite.dat",
			}, t.TempDir(), "", releaseInfo{}, func(p download.Progress) {
				mu.Lock()
				latest[p.Name] = p
				mu.Unlock()
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"geoip.dat", "geosite.dat"} {
				p := latest[name]
				if !p.Done || p.Received != int64(len(validGeoFile(name))) ||
					(known && p.Total != p.Received) ||
					(!known && p.Total != 0) {
					t.Fatalf("invalid %s progress: %+v", name, p)
				}
			}
		})
	}
}

func TestCancelFromGeoProgressPreservesBothFiles(t *testing.T) {
	dir := t.TempDir()
	writeOldGeoFiles(t, dir)
	withHTTPClient(t, func(request *http.Request) (*http.Response, error) {
		return response(http.StatusOK, validGeoFile(filepath.Base(request.URL.Path))), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := updateGeoFilesWithMetadata(ctx, []string{
		"https://example.test/geoip.dat", "https://example.test/geosite.dat",
	}, dir, "", releaseInfo{}, func(p download.Progress) {
		if p.Received > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Geo update = %v", err)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "old-"+name {
			t.Fatalf("old %s changed: %q, %v", name, data, err)
		}
	}
}
