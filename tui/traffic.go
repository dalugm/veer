package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dalugm/veer/session"
)

type (
	trafficPoint   struct{ up, down float64 }
	trafficHistory struct {
		points           []trafficPoint
		upload, download uint64
		at, since        time.Time
		pid              int
	}
)

// add uses cumulative counter differences and the actual sampling interval.
// A first sample (or counter reset) establishes a baseline, never a rate.
func (h *trafficHistory) add(upload, download uint64, at time.Time) {
	if at.IsZero() || !at.After(h.at) {
		return
	}
	if !h.at.IsZero() && upload >= h.upload && download >= h.download {
		seconds := at.Sub(h.at).Seconds()
		h.points = append(
			h.points,
			trafficPoint{
				float64(upload-h.upload) / seconds,
				float64(download-h.download) / seconds,
			},
		)
		if len(h.points) > 120 {
			copy(h.points, h.points[len(h.points)-120:])
			h.points = h.points[:120]
		}
	} else {
		h.points = nil
	}
	h.upload, h.download, h.at = upload, download, at
}

func (m *Model) observeTraffic(now time.Time) {
	m.now = now
	s := m.snapshot
	if !m.traffic.since.Equal(s.Since) || m.traffic.pid != s.PID {
		m.traffic = trafficHistory{since: s.Since, pid: s.PID}
	}
	if s.State == session.Running {
		m.traffic.add(s.Traffic.Upload, s.Traffic.Download, s.Traffic.At)
	}
}

func (m *Model) trafficStatus() string {
	if m.snapshot.State != session.Running {
		return "Offline"
	}
	if m.snapshot.TrafficError != "" {
		return "Unavailable"
	}
	if m.traffic.at.IsZero() {
		return "Waiting for samples"
	}
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	if now.Sub(m.traffic.at) > 3*time.Second {
		return "Stale"
	}
	if len(m.traffic.points) == 0 {
		return "Measuring"
	}
	return "Live"
}

func trafficBytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f B", value)
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

// trafficView gives the chart most of the available height. Its metrics share
// fixed columns so changing rates never move the labels or the plot.
func (m *Model) trafficView(w, h int) string {
	state := m.trafficStatus()
	up, down := "—", "—"
	if state == "Live" {
		last := m.traffic.points[len(m.traffic.points)-1]
		up, down = trafficBytes(last.up)+"/s", trafficBytes(last.down)+"/s"
	}
	upload := lipgloss.NewStyle().Foreground(purple)
	statusStyle := faint
	if state == "Live" {
		statusStyle = accent
	}
	if state == "Unavailable" || state == "Stale" {
		statusStyle = lipgloss.NewStyle().Foreground(rose)
	}
	title := strong.Render("TRAFFIC")
	status := statusStyle.Render("● " + state)
	lines := []string{spread(title, status, w)}
	column := (w - 3) / 2
	if h >= 12 && column >= 36 {
		lines = append(
			lines,
			"",
			spread(faint.Render("UPLOAD"), "", column)+"   "+faint.Render("DOWNLOAD"),
		)
	}
	visible := visibleTraffic(m.traffic.points, w)
	upPeak, downPeak := trafficPeaks(visible)
	peakUp, peakDown := "—", "—"
	if len(visible) > 0 {
		peakUp = trafficBytes(upPeak) + "/s"
		peakDown = trafficBytes(downPeak) + "/s"
	}
	upMetric := upload.Bold(true).Render("↑ "+up) + faint.Render(" · peak "+peakUp)
	downMetric := accent.Bold(true).Render("↓ "+down) + faint.Render(" · peak "+peakDown)
	if column >= 36 {
		lines = append(lines, spread(upMetric, "", column)+"   "+downMetric)
	} else {
		lines = append(lines, upMetric, downMetric)
	}
	upTotal, downTotal := "—", "—"
	if !m.traffic.at.IsZero() {
		upTotal = trafficBytes(float64(m.traffic.upload))
		downTotal = trafficBytes(float64(m.traffic.download))
	}
	lines = append(
		lines,
		spread(faint.Render("total  "+upTotal), "", column)+"   "+faint.Render("total  "+downTotal),
	)
	if h >= 12 {
		lines = append(lines, "")
	}
	graphHeight := h - len(lines)
	if graphHeight%2 == 0 {
		graphHeight--
	}
	if graphHeight >= 3 {
		lines = append(lines, trafficGraph(m.traffic.points, w, graphHeight)...)
	}
	return fit(strings.Join(lines, "\n"), w, h)
}

// spread aligns styled text by terminal cell width rather than byte length.
func spread(left, right string, width int) string {
	right = clip(right, width)
	left = clip(left, max(0, width-lipgloss.Width(right)))
	return left + strings.Repeat(
		" ",
		max(0, width-lipgloss.Width(left)-lipgloss.Width(right)),
	) + right
}

const trafficBarStride = 2 // One column for the bar, one blank column between bars.

func trafficCapacity(width int) int {
	return (max(1, width) + trafficBarStride - 1) / trafficBarStride
}

func visibleTraffic(points []trafficPoint, width int) []trafficPoint {
	return points[max(0, len(points)-trafficCapacity(width)):]
}

func trafficPeaks(points []trafficPoint) (up, down float64) {
	for _, p := range points {
		up = max(up, p.up)
		down = max(down, p.down)
	}
	return
}

func trafficGraph(points []trafficPoint, w, h int) []string {
	columns := max(1, w)
	points = visibleTraffic(points, w)
	upPeak, downPeak := trafficPeaks(points)
	top := (h - 1) / 2
	bottom := h - 1 - top
	// Anchor the newest sample at the right edge. Each new sample shifts all
	// previous bars left by one fixed slot; points beyond the left edge expire.
	positions := make(map[int]trafficPoint, len(points))
	for i, p := range points {
		col := columns - 1 - (len(points)-1-i)*trafficBarStride
		positions[col] = p
	}
	upload := lipgloss.NewStyle().Foreground(purple)
	grid := lipgloss.NewStyle().Foreground(border)
	lines := make([]string, 0, h)
	for row := range h {
		var bars strings.Builder
		for col := range columns {
			glyph := " "
			style := grid
			if row == top {
				glyph = "─"
			} else if p, ok := positions[col]; ok {
				value, peak, rows, distance := p.up, upPeak, top, top-row
				style = upload
				if row > top {
					value, peak, rows, distance = p.down, downPeak, bottom, row-top
					style = accent
				}
				if peak > 0 && value > 0 {
					// Two vertical pixels per cell keep small changes visible. Upload
					// fills from the bottom; download fills from the top.
					pixels := int(math.Round(value/peak*float64(rows*2))) - (distance-1)*2
					if pixels >= 2 {
						glyph = "█"
					} else if pixels == 1 {
						glyph = "▄"
						if row > top {
							glyph = "▀"
						}
					}
				}
			}
			bars.WriteString(style.Render(glyph))
		}
		lines = append(lines, bars.String())
	}
	return lines
}
