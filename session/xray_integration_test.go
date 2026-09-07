package session

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
)

func TestNativeXrayLocalHTTPProxy(t *testing.T) {
	binary := os.Getenv("VEER_TEST_XRAY")
	if binary == "" {
		t.Skip("set VEER_TEST_XRAY to test an installed official core")
	}
	target := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "veer-native-proxy-ok") },
		),
	)
	defer target.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	path := filepath.Join(t.TempDir(), "direct.json")
	if err := os.WriteFile(
		path,
		[]byte(
			fmt.Sprintf(
				`{"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"http"}],"outbounds":[{"protocol":"freedom"}]}`,
				port,
			),
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	c := New(engine.Xray{})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := c.Start(t.Context(), engine.Options{Binary: binary, Config: path}); err != nil {
		t.Fatalf("start: %v logs: %v", err, c.Snapshot().Logs)
	}
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "veer-native-proxy-ok" {
		t.Fatalf("response=%q err=%v", body, err)
	}
	deadline := time.After(5 * time.Second)
	for {
		s := c.Snapshot()
		if s.Traffic.Upload > 0 && s.Traffic.Download >= uint64(len(body)) &&
			!s.Traffic.At.IsZero() {
			break
		}
		select {
		case <-deadline:
			t.Fatalf(
				"no native traffic counters: %+v, error: %s, logs: %v",
				s.Traffic,
				s.TrafficError,
				s.Logs,
			)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
