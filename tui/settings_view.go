package tui

import (
	"runtime"
	"strings"

	update "github.com/dalugm/veer/engine/coreupdate"
)

func (m *Model) coreLabel() string {
	version := m.version
	if m.running() {
		version = m.runningVersion
		if version == "" {
			version = "Unavailable"
		}
	}
	if parsed := update.ParseVersion(version); parsed != "" {
		return "Xray " + strings.TrimPrefix(parsed, "v")
	}
	fields := strings.Fields(safe(version))
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Xray") {
		return "Xray " + fields[1]
	}
	return "Xray · " + safe(version)
}

func (m *Model) dnsDescription() string {
	if runtime.GOOS == "linux" && m.info.TUN && m.info.TUNSystemDNS {
		return "Core-managed · TUN gateway"
	}
	switch m.config.DNSMode {
	case "off":
		return "Off (system unchanged)"
	case "custom":
		return "Custom · " + safe(m.config.DNS)
	default:
		if runtime.GOOS == "windows" {
			return "Automatic · core-managed (system unchanged)"
		}
		return "Automatic · 1.1.1.1, 8.8.8.8"
	}
}

func (m *Model) toolsView(w, h int) string {
	return box(
		"TOOLS",
		accent.Bold(true).
			Render("[ g ]  Geo assets    [ u ]  Xray update")+
			"\n\n"+m.geoAssetsView(h < 14)+"\n\n"+faint.Render("Downloads happen only when you request them."),
		w,
		h,
	)
}

func (m *Model) settingsView(w, h int) string {
	geo := m.config.GeoDir
	if geo == "" {
		geo = "Config directory"
	}
	dns := m.dnsDescription()
	service := m.config.NetworkService
	if service == "" {
		service = "Automatic (active default route)"
	}
	body := faint.Render(
		"XRAY",
	) + "    " + safe(
		m.config.EnginePath,
	) + "\n\n" + faint.Render(
		"VERSION",
	) + " " + safe(
		m.version,
	) + "\n\n" + faint.Render(
		"GEO",
	) + "     " + safe(
		geo,
	) + "\n" + faint.Render(
		"TUN DNS",
	) + " " + safe(
		dns,
	) + "\n" + faint.Render(
		"SERVICE",
	) + " " + safe(
		service,
	) + "\n\n" + faint.Render(
		"SAVED TO",
	) + "\n" + safe(
		m.path,
	)
	return box("SETTINGS", body, w, h)
}
