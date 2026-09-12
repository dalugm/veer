package coreupdate

import "testing"

func TestCheckReturnsCompatibleVersionsInDescendingOrder(t *testing.T) {
	incomplete := releaseFixture("v4.0.0", false)
	incomplete.Assets = nil
	for _, tt := range []struct {
		channel Channel
		want    []string
	}{
		{Stable, []string{"v2.0.0", "v1.1.0"}},
		{Preview, []string{"v3.0.0-rc.10", "v3.0.0-rc.2", "v2.0.0", "v1.1.0"}},
	} {
		c := releaseClient(t, []githubRelease{
			releaseFixture("v1.0.0", false), releaseFixture("v1.1.0", false),
			releaseFixture("v3.0.0-rc.2", true), releaseFixture("v2.0.0", false),
			releaseFixture("v3.0.0-rc.10", true), releaseFixture("v1.1.0", false), incomplete,
		})
		got, err := c.Check(t.Context(), "v1.0.0", tt.channel)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(tt.want) {
			t.Fatalf("got %+v, want %v", got, tt.want)
		}
		for i, want := range tt.want {
			if got[i].Version != want {
				t.Fatalf("position %d: %s != %s", i, got[i].Version, want)
			}
		}
	}
}
