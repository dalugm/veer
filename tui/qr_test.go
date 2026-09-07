package tui

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/settings"
	qr "github.com/piglig/go-qr"
)

const qrOutbound = `{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"private-test-id"}]}]},"streamSettings":{"network":"tcp","security":"tls"}}`

func TestQRSharingAndNavigation(t *testing.T) {
	m := newTestModel(t)
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(
		path,
		[]byte(`{"outbounds":[`+qrOutbound+`,`+qrOutbound+`]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	m.config.Profiles = []settings.Profile{{ID: "test", Name: "Test", Path: path}}
	m.page = Profiles
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("missing QR command")
	}
	m.Update(cmd())
	if m.qr == nil || len(m.qr.items) != 2 {
		t.Fatal("missing outbound choices")
	}
	page := m.page
	press(m, 'l')
	if m.qr.index != 1 || m.page != page {
		t.Fatal("QR navigation changed page")
	}
	m.width, m.height = 160, 80
	view := m.View().Content
	if strings.Contains(view, "private-test-id") || !strings.Contains(view, "▀") {
		t.Fatal("missing QR or visible credential")
	}
	plain := ansi.Strip(view)
	for _, want := range []string{"V E E R", "Share QR", "Test · VLESS 2/2", "╭", "Esc/q close"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing modal context %q", want)
		}
	}
	modal := m.qrView(m.width, m.height)
	if strings.Count(modal, "\n") >= m.height-1 {
		t.Fatal("QR modal expanded to fullscreen")
	}
	for _, line := range strings.Split(ansi.Strip(renderQR(m.qr.items[1].code)), "\n") {
		if !strings.Contains(plain, line) {
			t.Fatal("QR row or quiet zone cropped by overlay")
		}
	}
	// Decode the actual terminal glyphs, including quiet zone, to catch flipped
	// half-blocks, polarity errors, or malformed row packing in our renderer.
	lines := strings.Split(ansi.Strip(renderQR(m.qr.items[0].code)), "\n")
	const scale = 8
	img := image.NewRGBA(image.Rect(0, 0, len([]rune(lines[0]))*scale, len(lines)*2*scale))
	for y, line := range lines {
		for x, c := range []rune(line) {
			for half := range 2 {
				dark := c == '█' || (c == '▀' && half == 0) || (c == '▄' && half == 1)
				shade := color.White
				if dark {
					shade = color.Black
				}
				for dy := range scale {
					for dx := range scale {
						img.Set(x*scale+dx, (y*2+half)*scale+dy, shade)
					}
				}
			}
		}
	}
	decoded, err := qr.Decode(img)
	if err != nil || !strings.HasPrefix(decoded, "vless://private-test-id@example.com:443?") {
		t.Fatalf("round trip: %v", err)
	}
	m.width, m.height = 60, 18
	if !strings.Contains(m.View().Content, "enlarge") || strings.Contains(m.View().Content, "▀") {
		t.Fatal("small terminal must not crop QR")
	}
	press(m, 'q')
	if m.qr != nil || m.quitting {
		t.Fatal("q should close QR only")
	}
}

func TestQRFailuresAndCancellation(t *testing.T) {
	for _, data := range []string{`bad`, `{"outbounds":[{"protocol":"freedom"}]}`} {
		path := filepath.Join(t.TempDir(), "profile.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadQR(context.Background(), settings.Profile{Path: path}); err == nil {
			t.Fatal("expected error")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadQR(ctx, settings.Profile{Path: "missing"}); err == nil {
		t.Fatal("expected cancellation")
	}
}
