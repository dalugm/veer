package coreupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func releaseFixture(tag string, preview bool) githubRelease {
	return githubRelease{Tag: tag, Prerelease: preview, Assets: []asset{
		{Name: "Xray-linux-64.zip", Size: 10},
		{Name: "Xray-linux-64.zip.dgst", Size: 100},
	}}
}

func releaseClient(t *testing.T, releases []githubRelease) *Client {
	t.Helper()
	c := New()
	c.goos, c.goarch = "linux", "amd64"
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20&page=1" {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		b, err := json.Marshal(releases)
		if err != nil {
			t.Fatal(err)
		}
		return response(http.StatusOK, string(b)), nil
	})
	return c
}

func TestCheckChannelsAndVersionOrdering(t *testing.T) {
	releases := []githubRelease{
		releaseFixture("v1.2.0-rc.2", true), releaseFixture("v1.0.1", false),
		releaseFixture("v1.2.0-rc.10", true), releaseFixture("v1.1.0", false),
		releaseFixture("v9.0.0-beta.1", false), releaseFixture("v1.2.0", true),
		releaseFixture("bad\x1b[31m", false), releaseFixture("v1.2", false),
	}
	releases = append(releases, githubRelease{Tag: "v10.0.0", Draft: true})
	for _, tt := range []struct {
		name, current string
		channel       Channel
		want          string
	}{
		{"stable filters prerelease tags and flags", "v1.0.0", Stable, "v1.1.0"},
		{"preview includes unmarked prerelease tags", "v1.0.0", Preview, "v9.0.0-beta.1"},
		{"stable does not downgrade preview", "v1.2.0-rc.1", Stable, ""},
		{"equal ignores metadata", "v1.1.0+build.9", Stable, ""},
		{"newer local build", "v20.0.0", Preview, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := releaseClient(t, releases).Check(t.Context(), tt.current, tt.channel)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("unexpected update: %+v", got)
				}
				return
			}
			if got == nil || got[0].Version != tt.want {
				t.Fatalf("got %+v, want %s", got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		releases []githubRelease
		want     string
	}{
		{[]githubRelease{releaseFixture("v1.2.0-rc.2", true), releaseFixture("v1.2.0-rc.10", true)}, "v1.2.0-rc.10"},
		{[]githubRelease{releaseFixture("v1.2.0-rc.10", true), releaseFixture("v1.2.0", false)}, "v1.2.0"},
	} {
		got, err := releaseClient(t, tt.releases).Check(t.Context(), "1.1.0", Preview)
		if err != nil || got == nil || got[0].Version != tt.want {
			t.Fatalf("got %+v, %v; want %s", got, err, tt.want)
		}
	}
}

func TestCheckRejectsUnknownCurrentAndChannelWithoutRequest(t *testing.T) {
	c := releaseClient(t, nil)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid check reached network")
		return nil, errors.New("unexpected request")
	})
	for _, v := range []string{"dev", "", "(devel)", "v1", "v1.2"} {
		if _, err := c.Check(t.Context(), v, Stable); !errors.Is(err, ErrUnknownVersion) {
			t.Fatalf("%q: %v", v, err)
		}
	}
	if _, err := c.Check(t.Context(), "v1.0.0", "other"); err == nil {
		t.Fatal("accepted invalid channel")
	}
}

func TestCheckRequiresCompletePlatformAssets(t *testing.T) {
	for _, assets := range [][]asset{
		nil,
		{{Name: "Xray-linux-64.zip.dgst", Size: 100}},
		{{Name: "Xray-linux-64.zip", Size: 10}},
		{{Name: "Xray-windows-64.zip", Size: 10}, {Name: "Xray-linux-64.zip.dgst", Size: 100}},
		{{Name: "Xray-linux-64.zip", Size: maxBinarySize + 1}, {Name: "Xray-linux-64.zip.dgst", Size: 100}},
	} {
		r := releaseFixture("v1.1.0", false)
		r.Assets = assets
		if _, err := releaseClient(
			t,
			[]githubRelease{r},
		).Check(t.Context(), "v1.0.0", Stable); err == nil {
			t.Fatalf("accepted incomplete release: %+v", assets)
		}
	}
	c := releaseClient(t, []githubRelease{{Tag: "v1.1.0", Assets: []asset{
		{Name: "Xray-windows-arm64-v8a.zip", Size: 10}, {Name: "Xray-linux-64.zip.dgst", Size: 100},
	}}})
	c.goos, c.goarch = "windows", "arm64"
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		b, _ := json.Marshal(
			[]githubRelease{
				{
					Tag: "v1.1.0",
					Assets: []asset{
						{Name: "Xray-windows-arm64-v8a.zip", Size: 10},
						{Name: "Xray-windows-arm64-v8a.zip.dgst", Size: 100},
					},
				},
			},
		)
		return response(200, string(b)), nil
	})
	if r, err := c.Check(t.Context(), "v1.0.0", Stable); err != nil || r == nil {
		t.Fatalf("Windows asset: %+v %v", r, err)
	}
}

func TestCheckPaginationAndFailures(t *testing.T) {
	c := New()
	c.goos, c.goarch = "linux", "amd64"
	pages := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		pages++
		if r.URL.Query().Get("page") != fmt.Sprint(pages) {
			t.Fatal(r.URL)
		}
		releases := []githubRelease{releaseFixture("v2.0.0", false)}
		if pages == 1 {
			releases = make([]githubRelease, 20)
			for i := range releases {
				releases[i] = releaseFixture("v1.0.1", false)
			}
		}
		b, _ := json.Marshal(releases)
		return response(200, string(b)), nil
	})
	if r, err := c.Check(
		t.Context(),
		"v1.0.0",
		Stable,
	); err != nil || r == nil ||
		r[0].Version != "v2.0.0" {
		t.Fatalf("pagination: %+v %v", r, err)
	}
	for _, status := range []int{403, 404, 429, 500} {
		c.http.Transport = roundTripFunc(
			func(*http.Request) (*http.Response, error) { return response(status, "secret server body"), nil },
		)
		_, err := c.Check(t.Context(), "v1.0.0", Stable)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	c.http.Transport = roundTripFunc(
		func(*http.Request) (*http.Response, error) { return response(200, "not json"), nil },
	)
	if _, err := c.Check(t.Context(), "v1.0.0", Stable); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Check(ctx, "v1.0.0", Stable); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
