package engine

import (
	"context"
	"errors"
	"testing"
)

var (
	siteFixture = []byte("\x0a\x15\x0a\x02CN\x12\x0f\x08\x02\x12\x0bexample.com")
	ipFixture   = []byte("\x0a\x0e\x0a\x02CN\x12\x08\x0a\x04\xc0\x00\x02\x00\x10\x18")
)

func TestValidateGeoData(t *testing.T) {
	for _, tc := range []struct {
		label, name string
		data        []byte
		valid       bool
	}{
		{"domain", "geosite.dat", siteFixture, true},
		{"CIDR", "geoip.dat", ipFixture, true},
		{"html", "geosite.dat", []byte("<html>error</html>"), false},
		{"truncated", "geosite.dat", siteFixture[:len(siteFixture)-1], false},
		{"empty", "geoip.dat", nil, false},
		{"wrong format", "geoip.dat", siteFixture, false},
		{"no entries", "geosite.dat", []byte("\x08\x01"), false},
		{"unknown field", "geosite.dat", append(append([]byte(nil), siteFixture...), 0x18, 0x01), true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateGeoData(t.Context(), tc.name, tc.data)
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid %v", err, tc.valid)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ValidateGeoData(ctx, "geosite.dat", siteFixture); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func FuzzValidateGeoData(f *testing.F) {
	f.Add("geosite.dat", siteFixture)
	f.Add("geoip.dat", ipFixture)
	f.Add("geosite.dat", []byte("<html>error</html>"))
	f.Fuzz(
		func(t *testing.T, name string, data []byte) { _ = ValidateGeoData(t.Context(), name, data) },
	)
}
