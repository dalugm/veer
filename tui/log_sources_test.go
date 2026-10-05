package tui

import "testing"

func TestFileLogsPreserveSourceAndComponent(t *testing.T) {
	got := parseLog("[error] 2026/10/05 01:28:00.123 [Warning] proxy/vless: failed")
	if got.time != "01:28:00" || got.level != "WARN" || got.source != "error" ||
		got.message != "proxy/vless: failed" {
		t.Fatalf("%+v", got)
	}
}
