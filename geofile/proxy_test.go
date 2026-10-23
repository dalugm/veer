package geofile

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeoProxyEnvironment(t *testing.T) {
	// net/http caches proxy environment variables. Use a fresh process so
	// other requests and the developer's proxy settings cannot affect this test.
	if os.Getenv("VEER_TEST_GEO_PROXY") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), binary, "-test.run=^TestGeoProxyEnvironment$")
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			switch strings.ToUpper(name) {
			case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REQUEST_METHOD", "VEER_TEST_GEO_PROXY":
				continue
			}
			cmd.Env = append(cmd.Env, entry)
		}
		cmd.Env = append(cmd.Env, "VEER_TEST_GEO_PROXY=1",
			"HTTP_PROXY=http://127.0.0.1:18080", "HTTPS_PROXY=http://127.0.0.1:18081",
			"NO_PROXY=bypass.test")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("proxy subprocess: %v\n%s", err, output)
		}
		return
	}

	for _, tt := range []struct{ address, wantDial string }{
		{"http://geo.test/geoip.dat", "127.0.0.1:18080"},
		{"https://geo.test/geoip.dat", "127.0.0.1:18081"},
		{"https://bypass.test/geoip.dat", "bypass.test:443"},
		{"https://127.0.0.1/geoip.dat", "127.0.0.1:443"},
	} {
		t.Run(tt.address, func(t *testing.T) {
			original := httpClient
			t.Cleanup(func() { httpClient = original })
			transport := original.Transport.(*http.Transport).Clone()
			t.Cleanup(transport.CloseIdleConnections)
			dialed := make(chan string, 1)
			transport.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
				dialed <- address
				return nil, errors.New("test dial boundary")
			}
			httpClient = &http.Client{Transport: transport, Timeout: original.Timeout}
			if err := downloadFile(
				t.Context(),
				tt.address,
				filepath.Join(t.TempDir(), "geoip.dat"),
				nil,
			); err == nil {
				t.Fatal("unexpected success past the dial boundary")
			}
			select {
			case address := <-dialed:
				if address != tt.wantDial {
					t.Fatalf("dialed %q, want %q", address, tt.wantDial)
				}
			default:
				t.Fatal("request never reached the dial boundary")
			}
		})
	}
}
