package coreupdate

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"golang.org/x/mod/semver"
)

// LocalChannel identifies explicit prerelease versions without network access.
// A numeric version alone cannot establish Stable: GitHub may mark it prerelease.
func LocalChannel(output string) Channel {
	if v := ParseVersion(output); v != "" && semver.Prerelease(v) != "" {
		return Preview
	}
	return ""
}

// InstalledChannel looks up the installed version's official release status.
// It does not infer the status from the user's selected update channel.
func (c *Client) InstalledChannel(ctx context.Context, output string) (Channel, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v := ParseVersion(output)
	if v == "" {
		return "", ErrUnknownVersion
	}
	if channel := LocalChannel(output); channel != "" {
		return channel, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := c.read(
		ctx,
		"https://api.github.com/repos/"+repository+"/releases/tags/"+url.PathEscape(v),
		maxReleasesSize,
	)
	if err != nil {
		return "", err
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", errors.New("invalid installed release response")
	}
	if release.Draft || normalizeVersion(release.Tag) != v {
		return "", errors.New("installed version does not match a published release")
	}
	if release.Prerelease {
		return Preview, nil
	}
	return Stable, nil
}
