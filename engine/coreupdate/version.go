// Package coreupdate downloads and installs official Xray core releases.
package coreupdate

import (
	"errors"
	"strings"

	"golang.org/x/mod/semver"
)

// ErrUnknownVersion means the installed Xray has no comparable version.
var ErrUnknownVersion = errors.New(
	"cannot determine the installed Xray version; check the core path in Settings",
)

// Channel controls which releases may be offered, never allowing downgrades.
type Channel string

// Stable excludes prereleases; Preview includes prereleases and stable releases.
const (
	Stable  Channel = "stable"
	Preview Channel = "preview"
)

// ParseVersion extracts a comparable version from Xray's version command.
func ParseVersion(output string) string {
	version, _ := versionParts(output)
	return version
}

func versionParts(output string) (string, bool) {
	fields := strings.Fields(output)
	if len(fields) >= 2 && fields[0] == "Xray" {
		numeric := normalizeVersion(fields[1])
		if numeric == "" {
			return "", false
		}
		// Official builds put git describe's release tag after the codename.
		// Preserve its prerelease suffix instead of treating every RC as stable.
		if _, tail, ok := strings.Cut(output, ") "); ok {
			build := strings.Fields(tail)
			if len(build) > 0 {
				if tag := normalizeVersion(build[0]); tag != "" {
					if versionCore(tag) != versionCore(numeric) {
						return "", true
					}
					return tag, true
				}
			}
		}
		return numeric, false
	}
	return normalizeVersion(output), true
}

func versionCore(v string) string {
	return strings.SplitN(strings.SplitN(v, "+", 2)[0], "-", 2)[0]
}

func matchesRelease(output, release string) bool {
	v, explicit := versionParts(output)
	if v == "" {
		return false
	}
	if semver.Compare(v, release) == 0 {
		return true
	}
	return !explicit && semver.Prerelease(v) == "" && versionCore(v) == versionCore(release)
}

func normalizeVersion(v string) string {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if len(v) > 128 || !semver.IsValid(v) {
		return ""
	}
	core, _, _ := strings.Cut(strings.SplitN(v, "+", 2)[0], "-")
	if strings.Count(core, ".") != 2 {
		return ""
	}
	return v
}
