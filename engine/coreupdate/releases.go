package coreupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"time"

	"github.com/dalugm/veer/engine"
	"golang.org/x/mod/semver"
)

const (
	repository       = "XTLS/Xray-core"
	maxBinarySize    = 128 << 20
	maxChecksumsSize = 1 << 20
	maxReleasesSize  = 8 << 20
	releasesPerPage  = 20
)

// Client accesses official releases. Its operations are safe to run concurrently
// except Install, which the caller must serialize for a given executable.
type Client struct {
	http         *http.Client
	goos, goarch string
	version      func(context.Context, string) (string, error)
}

// New creates a client for the current operating system and architecture.
func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: releaseRedirect},
		goos: runtime.GOOS, goarch: runtime.GOARCH, version: engine.Version,
	}
}

func releaseRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many release redirects")
	}
	if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return errors.New("insecure release redirect")
	}
	switch req.URL.Hostname() {
	case "github.com",
		"api.github.com",
		"release-assets.githubusercontent.com",
		"objects.githubusercontent.com":
		return nil
	default:
		return errors.New("release redirected outside GitHub")
	}
}

type asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type githubRelease struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

// Release describes a newer compatible release returned by Check.
// Private download metadata prevents UI text from choosing arbitrary URLs.
type Release struct {
	Version    string
	Prerelease bool
	tag        string
	binary     asset
	checksums  asset
}

// Check returns compatible newer versions in descending order, or nil when current.
// It downloads only release metadata, never executables or checksum files.
func (c *Client) Check(ctx context.Context, current string, channel Channel) ([]Release, error) {
	current = ParseVersion(current)
	if current == "" {
		return nil, ErrUnknownVersion
	}
	if channel != Stable && channel != Preview {
		return nil, errors.New("invalid update channel")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var candidates []githubRelease
	for page := 1; ; page++ {
		if page > 50 {
			return nil, errors.New("release history exceeds lookup limit")
		}
		data, err := c.read(
			ctx,
			fmt.Sprintf(
				"https://api.github.com/repos/%s/releases?per_page=%d&page=%d",
				repository,
				releasesPerPage,
				page,
			),
			maxReleasesSize,
		)
		if err != nil {
			return nil, fmt.Errorf("check Xray releases: %w", err)
		}
		var releases []githubRelease
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, errors.New("invalid release response")
		}
		for _, r := range releases {
			v := normalizeVersion(r.Tag)
			if r.Draft || v == "" || semver.Compare(v, current) <= 0 {
				continue
			}
			if channel == Stable && (r.Prerelease || semver.Prerelease(v) != "") {
				continue
			}
			candidates = append(candidates, r)
		}
		if len(releases) < releasesPerPage {
			break
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	name, err := c.archiveName()
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(candidates, func(a, b githubRelease) int {
		return semver.Compare(normalizeVersion(b.Tag), normalizeVersion(a.Tag))
	})
	var result []Release
	var unavailable error
	seen := make(map[string]bool)
	for _, r := range candidates {
		v := normalizeVersion(r.Tag)
		key := semver.Canonical(v)
		if seen[key] {
			continue
		}
		binary, err := findAsset(r.Assets, name, maxBinarySize)
		if err != nil {
			if unavailable == nil {
				unavailable = err
			}
			continue
		}
		checksums, err := findAsset(r.Assets, name+".dgst", maxChecksumsSize)
		if err != nil {
			if unavailable == nil {
				unavailable = err
			}
			continue
		}
		seen[key] = true
		result = append(
			result,
			Release{
				Version:    v,
				Prerelease: r.Prerelease || semver.Prerelease(v) != "",
				tag:        r.Tag,
				binary:     binary,
				checksums:  checksums,
			},
		)
	}
	if len(result) == 0 {
		return nil, unavailable
	}
	return result, nil
}

func (c *Client) archiveName() (string, error) {
	osName := c.goos
	switch osName {
	case "darwin":
		osName = "macos"
	case "linux", "windows":
	default:
		return "", fmt.Errorf("unsupported Xray update platform: %s/%s", c.goos, c.goarch)
	}
	arch := ""
	switch c.goarch {
	case "amd64":
		arch = "64"
	case "arm64":
		arch = "arm64-v8a"
	case "386":
		if c.goos != "darwin" {
			arch = "32"
		}
	}
	if arch == "" {
		return "", fmt.Errorf("unsupported Xray update architecture: %s/%s", c.goos, c.goarch)
	}
	return "Xray-" + osName + "-" + arch + ".zip", nil
}

func findAsset(assets []asset, name string, limit int64) (asset, error) {
	var found asset
	for _, a := range assets {
		if a.Name != name {
			continue
		}
		if found.Name != "" {
			return asset{}, fmt.Errorf("release has duplicate %s", name)
		}
		if a.Size <= 0 || a.Size > limit {
			return asset{}, fmt.Errorf("invalid release asset size for %s", name)
		}
		found = a
	}
	if found.Name == "" {
		return asset{}, fmt.Errorf("release does not provide %s", name)
	}
	return found, nil
}

func assetURL(tag, name string) string {
	return "https://github.com/" + repository + "/releases/download/" + url.PathEscape(
		tag,
	) + "/" + url.PathEscape(
		name,
	)
}

func (c *Client) get(ctx context.Context, address string) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Veer")
	if req.URL.Hostname() == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) read(ctx context.Context, address string, limit int64) ([]byte, error) {
	resp, err := c.get(ctx, address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release response exceeds size limit")
	}
	return data, ctx.Err()
}
