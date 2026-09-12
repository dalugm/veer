package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dalugm/veer/settings"
	"github.com/dalugm/veer/subscription"
	qr "github.com/piglig/go-qr"
)

type qrItem struct {
	code *qr.QrCode
	err  string
}
type qrModal struct {
	name  string
	items []qrItem
	index int
}
type qrMsg struct {
	modal *qrModal
	err   error
}

func (m *Model) openQR() tea.Cmd {
	var p settings.Profile
	var ok bool
	switch m.page {
	case Overview:
		p, ok = m.config.Active()
	case Profiles:
		if m.hasFocusedProfile() {
			p, ok = m.config.Profiles[m.cursor], true
		}
	default:
		return nil
	}
	if !ok {
		m.bad = true
		m.notice = "Choose a profile to share."
		return nil
	}
	ctx, cancel := m.begin("Preparing QR code")
	return m.workers.track(func() tea.Msg {
		defer cancel()
		modal, err := loadQR(ctx, p)
		return qrMsg{modal, err}
	})
}

func loadQR(ctx context.Context, p settings.Profile) (*qrModal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, errors.New("cannot read profile for sharing")
	}
	defer func() { _ = f.Close() }()
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot share profile: unreadable or larger than 4 MiB")
	}
	var config subscription.XrayConfig
	if json.Unmarshal(data, &config) != nil {
		return nil, errors.New("cannot share profile: invalid JSON")
	}
	modal := &qrModal{name: safe(p.Name)}
	for _, out := range config.Outbounds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if out.Protocol != "vless" {
			continue
		}
		// Convert each outbound separately; an empty or repeated tag must not select
		// another outbound accidentally. Raw URIs and conversion errors stay private.
		single := subscription.XrayConfig{Outbounds: []subscription.Outbound{out}}
		link, err := single.ToVLESSLink("", p.Name)
		item := qrItem{}
		if err != nil {
			item.err = "This VLESS outbound cannot be represented as a share link."
		} else {
			item.code, err = qr.EncodeText(link, qr.Low)
			if err != nil {
				item.err = "Share link is too long to encode as a QR code."
			}
		}
		modal.items = append(modal.items, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(modal.items) == 0 {
		return nil, errors.New("QR sharing currently supports VLESS outbounds only")
	}
	return modal, nil
}

func (m *Model) qrKey(key string) {
	m.pendingG = false
	switch key {
	case "esc", "q", "y":
		m.qr = nil
	case "h", "k", "left", "up":
		m.qr.index = (m.qr.index + len(m.qr.items) - 1) % len(m.qr.items)
	case "l", "j", "right", "down":
		m.qr.index = (m.qr.index + 1) % len(m.qr.items)
	}
}

func (m *Model) qrView(w, h int) string {
	item := m.qr.items[m.qr.index]
	width := min(64, max(1, w-2))
	body := item.err
	footer := "Esc / q close"
	if len(m.qr.items) > 1 {
		footer = "h/l outbound · Esc/q close"
	}
	label := fmt.Sprintf("%s · VLESS %d/%d", m.qr.name, m.qr.index+1, len(m.qr.items))
	if item.code != nil {
		cols := item.code.Size() + 8
		rows := (cols + 1) / 2
		wanted := max(44, cols+4)
		if w < wanted+2 || h < rows+8 {
			body = fmt.Sprintf(
				"Please enlarge the terminal to\n%d × %d to show the complete QR code.",
				wanted+2,
				rows+8,
			)
		} else {
			width = wanted
			body = renderQR(item.code)
		}
	}
	inner := max(1, width-4)
	title := strong.Render("Share QR")
	body = lipgloss.PlaceHorizontal(inner, lipgloss.Center, body)
	label = lipgloss.PlaceHorizontal(inner, lipgloss.Center, faint.Render(clip(label, inner)))
	footer = lipgloss.PlaceHorizontal(inner, lipgloss.Center, faint.Render(clip(footer, inner)))
	content := title + "\n\n" + body + "\n" + label + "\n" + footer
	card := base.Background(lipgloss.Color("#111827")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width).
		Render(content)
	card = fit(card, min(w, lipgloss.Width(card)), min(h, lipgloss.Height(card)))
	// Nested ANSI resets and border styling leave some cells without a
	// background. Make the modal opaque without changing the QR's light cells.
	canvas := lipgloss.NewCanvas(lipgloss.Width(card), lipgloss.Height(card)).
		Compose(lipgloss.NewLayer(card))
	for y := range canvas.Height() {
		for x := range canvas.Width() {
			if cell := canvas.CellAt(x, y); cell != nil && cell.Style.Bg == nil {
				filled := *cell
				filled.Style.Bg = lipgloss.Color("#111827")
				canvas.SetCell(x, y, &filled)
			}
		}
	}
	return canvas.Render()
}

// renderQR packs two module rows into each cell and keeps a four-module quiet
// zone. Explicit dark-on-light colors keep it scannable independent of terminal theme.
func renderQR(code *qr.QrCode) string {
	var b strings.Builder
	size := code.Size() + 8
	for y := 0; y < size; y += 2 {
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("\x1b[38;2;17;24;39;48;2;220;228;242m")
		for x := 0; x < size; x++ {
			top, bottom := code.Module(x-4, y-4), code.Module(x-4, y-3)
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
