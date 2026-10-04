package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/download"
	update "github.com/dalugm/veer/engine/coreupdate"
)

type progressUpdater struct {
	*fakeUpdater
	install func(context.Context, func(download.Progress)) error
}

func (f progressUpdater) Install(
	ctx context.Context,
	binary, current string,
	release update.Release,
	report func(download.Progress),
) (update.Result, error) {
	if err := f.install(ctx, report); err != nil {
		return update.Result{}, err
	}
	return f.fakeUpdater.Install(ctx, binary, current, release, report)
}

func footerContent(m *Model) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(m.View().Content), "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-3):], "\n")
}

func TestXrayDownloadProgressAndCancellation(t *testing.T) {
	for _, cancelDownload := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "success", true: "cancel"}[cancelDownload],
			func(t *testing.T) {
				m, fake := updaterModel(t)
				m.updates.open = true
				runCommands(m, m.checkUpdate())
				started, finish := make(chan struct{}), make(chan struct{})
				m.updates.client = progressUpdater{
					fakeUpdater: fake,
					install: func(ctx context.Context, report func(download.Progress)) error {
						report(download.Progress{Name: "Xray", Received: 1 << 20, Total: 4 << 20})
						close(started)
						select {
						case <-finish:
							report(
								download.Progress{
									Name:     "Xray",
									Received: 4 << 20,
									Total:    4 << 20,
									Done:     true,
								},
							)
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					},
				}
				cmd := m.installUpdate()
				cancel := m.cancelWork
				t.Cleanup(cancel)
				result := make(chan tea.Msg, 1)
				go func() { result <- cmd() }()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("download did not start")
				}
				for _, size := range [][2]int{{60, 18}, {80, 24}, {100, 32}} {
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					footer := footerContent(m)
					for _, want := range []string{"Xray [", "25%", "1.0 MiB/4.0 MiB"} {
						if !strings.Contains(footer, want) {
							t.Fatalf("missing bottom progress %q:\n%s", want, m.View().Content)
						}
					}
					for line := range strings.SplitSeq(m.View().Content, "\n") {
						if ansi.StringWidth(line) > size[0] {
							t.Fatalf("progress overflows terminal: %q", line)
						}
					}
				}
				if cancelDownload {
					m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
					if !strings.Contains(footerContent(m), "Cancelling") {
						t.Fatal("cancellation status hidden by progress bar")
					}
				} else {
					close(finish)
				}
				select {
				case msg := <-result:
					m.Update(msg)
				case <-time.After(3 * time.Second):
					t.Fatal("download did not finish")
				}
				if m.busy || m.downloads != nil || strings.Contains(footerContent(m), "Xray [") ||
					m.bad != cancelDownload {
					t.Fatalf("progress not cleared after result: %s", footerContent(m))
				}
			},
		)
	}
}

func TestGeoProgressCombinesFilesAndIgnoresPreviousWorker(t *testing.T) {
	m := newTestModel(t)
	_, cancel := m.begin("Downloading Geo assets…")
	defer cancel()
	report := m.startDownload("Geo")
	report(download.Progress{Name: "geoip.dat", Received: 1 << 20, Total: 1 << 20, Done: true})
	report(download.Progress{Name: "geosite.dat", Received: 1 << 20})
	footer := footerContent(m)
	if !strings.Contains(footer, "Geo [") || !strings.Contains(footer, "2.0 MiB") ||
		strings.Contains(footer, "%") {
		t.Fatalf("unknown total pretends to have a percentage: %s", footer)
	}
	before := footer
	m.phase++
	if footerContent(m) == before {
		t.Fatal("unknown-size progress does not animate")
	}
	report(download.Progress{Name: "geosite.dat", Received: 1 << 20, Total: 3 << 20})
	if footer := footerContent(m); !strings.Contains(footer, "50% 2.0 MiB/4.0 MiB") {
		t.Fatalf("file progress was not combined: %s", footer)
	}
	report(download.Progress{Name: "geosite.dat", Received: 3 << 20, Done: true})
	if footer := footerContent(m); !strings.Contains(footer, "Verifying… 100%") {
		t.Fatalf("missing verification phase: %s", footer)
	}
	m.Update(actionMsg{notice: "Geo assets updated."})
	_, nextCancel := m.begin("Downloading Xray…")
	defer nextCancel()
	next := m.startDownload("Xray")
	next(download.Progress{Name: "Xray", Received: 1 << 20, Total: 4 << 20})
	report(download.Progress{Name: "geosite.dat", Received: 4 << 20, Done: true})
	if footer := footerContent(m); !strings.Contains(footer, "25% 1.0 MiB/4.0 MiB") {
		t.Fatalf("late worker changed a newer download: %s", footer)
	}
}

func TestGeoFormShowsProgressBeforeIOAndClearsOnCancellation(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	m.form.inputs[0].SetValue(t.TempDir())
	cmd := m.submitForm()
	if cmd == nil || !strings.Contains(footerContent(m), "Geo [") {
		t.Fatal("Geo form did not show initial progress")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(cmd())
	if m.busy || m.downloads != nil || !m.bad || m.form == nil {
		t.Fatal("cancelled Geo operation retained progress or closed its form")
	}
}
