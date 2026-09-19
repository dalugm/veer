package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

func TestOverviewTrafficFits(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, required := range []string{"TRAFFIC", "↑", "↓", "? help"} {
			if !strings.Contains(view, required) {
				t.Errorf("%v missing %s:\n%s", size, required, view)
			}
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height %d > %d", len(lines), size[1])
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Errorf("overflow: %q", line)
			}
		}
	}
}

func TestTrafficRatesAndResets(t *testing.T) {
	h := trafficHistory{}
	now := time.Unix(100, 0)
	h.add(100, 200, now)
	if len(h.points) != 0 {
		t.Fatal("baseline became a rate")
	}
	h.add(400, 800, now.Add(1500*time.Millisecond))
	if len(h.points) != 1 || h.points[0].up != 200 || h.points[0].down != 400 {
		t.Fatalf("elapsed rates: %+v", h.points)
	}
	h.add(400, 800, now.Add(1500*time.Millisecond))
	if len(h.points) != 1 {
		t.Fatal("duplicate sample appended")
	}
	h.add(10, 20, now.Add(2*time.Second))
	if len(h.points) != 0 {
		t.Fatal("counter reset retained old rates")
	}
	for i := 1; i <= 130; i++ {
		h.add(uint64(10+i), uint64(20+i), now.Add(time.Duration(2+i)*time.Second))
	}
	if len(h.points) != 120 {
		t.Fatalf("history length %d", len(h.points))
	}
}

func TestTrafficAvailabilityAndReconnect(t *testing.T) {
	m := newTestModel(t)
	now := time.Unix(100, 0)
	m.snapshot = session.Snapshot{State: session.Running, PID: 10, Since: now}
	m.observeTraffic(now)
	if got := m.trafficStatus(); got != "Waiting for samples" {
		t.Fatal(got)
	}
	m.snapshot.Traffic = engine.Traffic{Upload: 100, Download: 200, At: now}
	m.observeTraffic(now)
	if got := m.trafficStatus(); got != "Measuring" {
		t.Fatal(got)
	}
	m.snapshot.Traffic = engine.Traffic{Upload: 110, Download: 220, At: now.Add(time.Second)}
	m.observeTraffic(now.Add(time.Second))
	if got := m.trafficStatus(); got != "Live" {
		t.Fatal(got)
	}
	m.observeTraffic(now.Add(5 * time.Second))
	if got := m.trafficStatus(); got != "Stale" {
		t.Fatal(got)
	}
	if got := ansi.Strip(m.trafficView(58, 8)); !strings.Contains(got, "↑ —") {
		t.Fatal(got)
	}
	m.snapshot.TrafficError = "rpc unavailable\x1b[31m"
	if got := m.trafficStatus(); got != "Unavailable" {
		t.Fatal(got)
	}
	m.snapshot.TrafficError = ""
	m.snapshot.Since = now.Add(6 * time.Second)
	m.snapshot.Traffic = engine.Traffic{Upload: 1, Download: 2, At: now.Add(6 * time.Second)}
	m.observeTraffic(now.Add(6 * time.Second))
	if len(m.traffic.points) != 0 || m.traffic.upload != 1 {
		t.Fatal("reconnect did not reset history")
	}
	m.snapshot.State = session.Stopped
	if got := m.trafficStatus(); got != "Offline" {
		t.Fatal(got)
	}
}

func TestTrafficGraphIndependentScalesAndRightAlignment(t *testing.T) {
	lines := trafficGraph([]trafficPoint{{up: 1, down: 10000}}, 20, 5)
	for i, line := range lines {
		line = ansi.Strip(line)
		if ansi.StringWidth(line) != 20 {
			t.Fatalf("width row %d: %q", i, line)
		}
		if i != 2 && !strings.HasSuffix(line, "█") {
			t.Fatalf("peak not at right edge: %q", line)
		}
		if i != 2 && strings.Count(line, "█") != 1 {
			t.Fatalf("invented data: %q", line)
		}
	}
}

func TestTrafficGraphUsesHalfCellPrecision(t *testing.T) {
	lines := trafficGraph([]trafficPoint{{up: 1, down: 100}, {up: 4, down: 400}}, 40, 5)
	view := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(view, "▄") || !strings.Contains(view, "▀") {
		t.Fatalf("fractional upload/download bars missing:\n%s", view)
	}
}

func TestTrafficGraphScrollsWithFixedGaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []float64
		want   string
	}{
		{"first", []float64{1}, "      █"},
		{"second shifts first left", []float64{1, 1}, "    █ █"},
		{"third", []float64{1, 1, 1}, "  █ █ █"},
		{"full", []float64{1, 1, 1, 1}, "█ █ █ █"},
		{"oldest leaves and no longer sets scale", []float64{100, 1, 1, 1, 1}, "█ █ █ █"},
		{"zero retains its slot", []float64{1, 0, 1}, "  █   █"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var points []trafficPoint
			for _, value := range tc.values {
				points = append(points, trafficPoint{up: value, down: value})
			}
			lines := trafficGraph(points, 7, 5)
			for _, row := range []int{0, 4} {
				text := []rune(ansi.Strip(lines[row]))
				if got := string(text[len(text)-7:]); got != tc.want {
					t.Fatalf("row %d: got %q want %q", row, got, tc.want)
				}
			}
		})
	}
}

func TestTrafficBarsKeepTheirOwnColors(t *testing.T) {
	lines := trafficGraph([]trafficPoint{{up: 10, down: 100}}, 40, 5)
	up := lipgloss.NewStyle().Foreground(purple).Render("█")
	down := lipgloss.NewStyle().Foreground(cyan).Render("█")
	if up == "█" || down == "█" || up == down {
		t.Fatal("expected distinct ANSI colors")
	}
	if !strings.Contains(lines[0], up) || !strings.Contains(lines[4], down) {
		t.Fatalf("bar color lost after axis reset: upload %q download %q", lines[0], lines[4])
	}
}

func TestSettingsDNSModeValidation(t *testing.T) {
	for _, tc := range []struct {
		mode, servers string
		valid         bool
	}{{"auto", "", true}, {"custom", "9.9.9.9", true}, {"off", "", true}, {"custom", "invalid", false}, {"typo", "", false}} {
		t.Run(tc.mode+tc.servers, func(t *testing.T) {
			m := newTestModel(t)
			m.openSettings()
			if len(m.form.inputs) != 5 {
				t.Fatalf("DNS mode field missing: %d", len(m.form.inputs))
			}
			m.form.inputs[2].SetValue(tc.mode)
			m.form.inputs[3].SetValue(tc.servers)
			cmd := m.submitForm()
			if cmd == nil {
				t.Fatal("no submit command")
			}
			m.Update(cmd())
			if m.bad == tc.valid {
				t.Fatalf("valid=%t bad=%t notice=%s", tc.valid, m.bad, m.notice)
			}
			if tc.valid && m.config.DNSMode != tc.mode {
				t.Fatalf("mode not saved: %s", m.config.DNSMode)
			}
			if !tc.valid && m.form == nil {
				t.Fatal("invalid form closed")
			}
		})
	}
}

func TestTrafficLivePreview(t *testing.T) {
	m := newTestModel(t)
	now := time.Unix(100, 0)
	m.snapshot = session.Snapshot{State: session.Running, PID: 42, Since: time.Now()}
	for i := range 16 {
		m.snapshot.Traffic = engine.Traffic{
			Upload:   uint64(i*i) * 1024,
			Download: uint64(i*i) * 10240,
			At:       now.Add(time.Duration(i) * time.Second),
		}
		m.observeTraffic(m.snapshot.Traffic.At)
	}
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		if strings.Contains(view, "samples ·") || strings.Contains(view, "polling") {
			t.Fatal("diagnostic footer remains")
		}
		if !strings.Contains(view, "29.0 KiB/s") || !strings.Contains(view, "290.0 KiB/s") ||
			!strings.Contains(view, "? help") {
			t.Fatal(view)
		}
		for line := range strings.SplitSeq(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("overflow %q", line)
			}
		}
		t.Logf("%dx%d\n%s", size[0], size[1], view)
	}
}

func TestTrafficPeakLabelsFollowVisibleWindow(t *testing.T) {
	m := newTestModel(t)
	m.traffic.points = []trafficPoint{{up: 999999, down: 999999}}
	for range 29 {
		m.traffic.points = append(m.traffic.points, trafficPoint{up: 1024, down: 2048})
	}
	for _, size := range [][2]int{{58, 7}, {78, 11}, {118, 23}} {
		view := ansi.Strip(m.trafficView(size[0], size[1]))
		if !strings.Contains(view, "peak") {
			t.Fatal("peak label missing")
		}
		if size[0] == 58 &&
			(!strings.Contains(view, "peak 1.0 KiB/s") || !strings.Contains(view, "peak 2.0 KiB/s") || strings.Contains(view, "976.6")) {
			t.Fatal("hidden history affected peak: " + view)
		}
		for line := range strings.SplitSeq(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("overflow: " + line)
			}
		}
	}
	graph := ansi.Strip(strings.Join(trafficGraph(m.traffic.points, 58, 5), "\n"))
	if strings.ContainsAny(graph, "0123456789│┼") {
		t.Fatal("left scale remains")
	}
}
