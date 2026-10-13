package engine

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// RequiredVersion returns the minimum core version for explicitly enabled features.
// Ordinary native configurations remain subject to Xray's own validation.
func (info Info) RequiredVersion(platform string) (version, feature string) {
	if info.LuaDNS || info.LuaRouting {
		return "v26.10.10", "DNS/routing Lua scripts"
	}
	if platform == "linux" && info.TUN && info.TUNSystemDNS {
		return "v26.9.30", "TUN autoSystemDnsToGateway"
	}
	return "", ""
}

// CheckVersion checks feature requirements against the core's version output.
// A feature requirement must be verifiable before starting the core.
func (info Info) CheckVersion(output, platform string) error {
	required, feature := info.RequiredVersion(platform)
	if required == "" {
		return nil
	}
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != "Xray" {
		return fmt.Errorf("cannot verify Xray version: %s requires %s or newer", feature, required)
	}
	version := "v" + strings.TrimPrefix(fields[1], "v")
	if !semver.IsValid(version) {
		return fmt.Errorf("cannot verify Xray version: %s requires %s or newer", feature, required)
	}
	if semver.Compare(version, required) < 0 {
		return fmt.Errorf("%s requires Xray %s or newer (installed %s)", feature, required, version)
	}
	return nil
}
