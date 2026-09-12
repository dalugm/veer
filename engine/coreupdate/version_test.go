package coreupdate

import "testing"

func TestParseXrayReleaseVersion(t *testing.T) {
	for _, tt := range []struct{ output, want string }{
		{"Xray 26.3.27 (Xray, Penetrates Everything.) v26.3.27 (go1.26 darwin/arm64)", "v26.3.27"},
		{"Xray 1.2.0 (Xray, Penetrates Everything.) v1.2.0-rc.2 (go1.26 windows/amd64)", "v1.2.0-rc.2"},
		{"Xray 1.2.0 (Xray, Penetrates Everything.) deadbee (go1.26 linux/amd64)", "v1.2.0"},
		{"Xray 1.2.0 (Xray, Penetrates Everything.) v9.0.0 (go1.26 linux/amd64)", ""},
		{"Xray 26.3.27", "v26.3.27"},
		{"unknown", ""},
	} {
		if got := ParseVersion(tt.output); got != tt.want {
			t.Fatalf("%q: got %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestActualXrayPrereleaseCanUpdateToNextRCAndStable(t *testing.T) {
	current := "Xray 1.2.0 (Xray, Penetrates Everything.) v1.2.0-rc.2 (go1.26 darwin/arm64)"
	for _, tt := range []struct {
		tag     string
		pre     bool
		channel Channel
	}{
		{"v1.2.0-rc.10", true, Preview}, {"v1.2.0", false, Stable},
	} {
		c := releaseClient(t, []githubRelease{releaseFixture(tt.tag, tt.pre)})
		r, err := c.Check(t.Context(), current, tt.channel)
		if err != nil || r == nil || r[0].Version != tt.tag {
			t.Fatalf("%s: %+v %v", tt.tag, r, err)
		}
	}
}
