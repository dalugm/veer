package coreupdate

import (
	"context"
	"net/http"
	"testing"
)

func TestInstalledChannel(t *testing.T) {
	for _, tt := range []struct {
		name, output, body string
		status             int
		want               Channel
		requests           int
	}{
		{"numeric preview", "Xray 26.10.21 (Xray, Penetrates Everything.) abcdef", `{"tag_name":"v26.10.21","prerelease":true}`, 200, Preview, 1},
		{"stable", "v26.10.21", `{"tag_name":"v26.10.21","prerelease":false}`, 200, Stable, 1},
		{"explicit rc", "Xray 26.10.21 (Xray, Penetrates Everything.) v26.10.21-rc.1 (go1.27)", "", 0, Preview, 0},
		{"unavailable", "v26.10.21", `{}`, 404, "", 1},
		{"wrong tag", "v26.10.21", `{"tag_name":"v26.10.20"}`, 200, "", 1},
		{"draft", "v26.10.21", `{"tag_name":"v26.10.21","draft":true}`, 200, "", 1},
		{"invalid", "unknown", "", 0, "", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			calls := 0
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://api.github.com/repos/XTLS/Xray-core/releases/tags/v26.10.21" {
					t.Fatalf("unexpected URL: %s", r.URL)
				}
				return response(tt.status, tt.body), nil
			})
			got, err := c.InstalledChannel(t.Context(), tt.output)
			if got != tt.want || (err != nil) != (tt.want == "") || calls != tt.requests {
				t.Fatalf("got %q, %v, %d requests", got, err, calls)
			}
		})
	}
	c := New()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.InstalledChannel(ctx, "v26.10.21"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
