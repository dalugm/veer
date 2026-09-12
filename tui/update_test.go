package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dalugm/veer/engine"
	update "github.com/dalugm/veer/engine/coreupdate"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

type fakeUpdater struct {
	checks, installs int
	restores         int
	channel          update.Channel
	releases         []update.Release
	err              error
	ctx              context.Context
	started          chan struct{}
}

func (f *fakeUpdater) Restore(
	_ context.Context,
	binary, backup, expectedTarget string,
) (update.Result, error) {
	f.restores++
	if binary != "xray" || backup != "/backup/previous.exe" || expectedTarget != "/install/xray" {
		return update.Result{}, errors.New("wrong restore target")
	}
	return update.Result{
		Version:    "v1.0.0",
		BackupPath: "/backup/newer.exe",
		TargetPath: "/install/xray",
	}, f.err
}

func (f *fakeUpdater) Check(
	ctx context.Context,
	current string,
	channel update.Channel,
) ([]update.Release, error) {
	f.checks++
	f.channel = channel
	f.ctx = ctx
	if f.started != nil {
		close(f.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if current != "v1.0.0" {
		return nil, errors.New("wrong current version")
	}
	return f.releases, f.err
}

func (f *fakeUpdater) Install(
	_ context.Context,
	binary, current string,
	r update.Release,
) (update.Result, error) {
	f.installs++
	if binary != "xray" || current != "v1.0.0" || r.Version != "v1.1.0" {
		return update.Result{}, errors.New("wrong update")
	}
	return update.Result{
		Version:    r.Version,
		BackupPath: "/backup/previous.exe",
		TargetPath: "/install/xray",
	}, f.err
}

func updaterModel(t *testing.T) (*Model, *fakeUpdater) {
	t.Helper()
	m := newTestModel(t)
	f := &fakeUpdater{releases: []update.Release{{Version: "v1.1.0"}}}
	m.updates.client, m.updates.current = f, "v1.0.0"
	m.updates.readVersion = func(context.Context, string) (string, error) { return "v1.0.0", nil }
	m.page = Tools
	return m, f
}

func runCommands(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runCommands(m, child)
		}
		return
	}
	_, next := m.Update(msg)
	if _, ticking := msg.(tickMsg); !ticking {
		runCommands(m, next)
	}
}

func TestUpdaterRequiresCheckAndExplicitConfirmation(t *testing.T) {
	m, f := updaterModel(t)
	press(m, 'u')
	if !m.updates.open || f.checks != 0 || f.installs != 0 {
		t.Fatal("opening updater caused network I/O")
	}
	press(m, 'u')
	if m.confirmation != "" || f.installs != 0 {
		t.Fatal("unchecked release offered")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if f.checks != 0 {
		t.Fatal("blocking check inside Update")
	}
	runCommands(m, cmd)
	if m.updates.latest == nil || f.channel != update.Stable || f.installs != 0 {
		t.Fatal("check failed or downloaded binary")
	}
	press(m, 'u')
	if m.confirmation != "update" || f.installs != 0 {
		t.Fatal("missing install confirmation")
	}
	press(m, 'n')
	if m.confirmation != "" || f.installs != 0 {
		t.Fatal("cancel installed update")
	}
	press(m, 'u')
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.installs != 0 {
		t.Fatal("blocking install inside Update")
	}
	runCommands(m, cmd)
	if f.installs != 1 || m.updates.current != "v1.1.0" || m.busy {
		t.Fatalf("install result: %+v", m.updates)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Updated") {
		t.Fatal("missing updated instruction")
	}
	press(m, 'u')
	if m.confirmation != "" {
		t.Fatal("installed candidate was retained")
	}
	if cmd := m.connect(); cmd != nil || m.bad {
		t.Fatal("new connection incorrectly blocked; with no profile it should open import")
	}
}

func TestOfflineRestoreRequiresConfirmationAndPersistsBackup(t *testing.T) {
	m, f := updaterModel(t)
	press(m, 'u')
	runCommands(m, m.checkUpdate(true))
	press(m, 'u')
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCommands(m, cmd)
	reopened, err := New(t.Context(), m.path, idleBackend{})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.availableBackup() != "/backup/previous.exe" {
		t.Fatal("backup lost after reopening")
	}
	reopened.updates.client = f
	reopened.page = Tools
	press(reopened, 'u')
	press(reopened, 'b')
	if reopened.confirmation != "restore-update" || f.restores != 0 {
		t.Fatal("restore bypassed confirmation")
	}
	press(reopened, 'n')
	if f.restores != 0 {
		t.Fatal("cancelled restore changed executable")
	}
	press(reopened, 'b')
	_, cmd = reopened.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCommands(reopened, cmd)
	if f.restores != 1 || f.checks != 1 || reopened.updates.current != "v1.0.0" {
		t.Fatal("offline restore failed or checked network")
	}
	c, err := settings.Load(m.path)
	if err != nil || c.CoreBackupPath != "/backup/newer.exe" {
		t.Fatal("replacement backup not persisted")
	}
	reopened.config.EnginePath = "another-xray"
	if reopened.availableBackup() != "" {
		t.Fatal("backup offered for unrelated core")
	}
}

func TestBackgroundUpdateCheckPreservesOperationAndIgnoresStaleChannel(t *testing.T) {
	m, _ := updaterModel(t)
	cmd := m.checkUpdate(false)
	m.busy, m.notice = true, "Connecting"
	m.Update(cmd())
	if !m.busy || m.notice != "Connecting" || m.updates.latest == nil {
		t.Fatal("background result interfered with operation")
	}
	m.busy = false
	old := m.checkUpdate(false)()
	m.config.CoreUpdateChannel = "preview"
	m.checkUpdate(false)
	m.Update(old)
	if m.updates.latest != nil || !m.updates.checking {
		t.Fatal("stale check changed current channel")
	}
}

func TestChannelPersistsAndRefreshesCandidate(t *testing.T) {
	m, f := updaterModel(t)
	press(m, 'u')
	if cmd := m.updateKey("left"); cmd != nil || m.config.CoreUpdateChannel != "stable" || m.busy {
		t.Fatal("selecting the default channel should do nothing")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.config.CoreUpdateChannel != "stable" {
		t.Fatal("channel changed before save completed")
	}
	runCommands(m, cmd)
	c, err := settings.Load(m.path)
	if err != nil || c.CoreUpdateChannel != "preview" || m.config.CoreUpdateChannel != "preview" ||
		f.channel != update.Preview {
		t.Fatalf("channel save: %+v %v", c, err)
	}
	if m.notice != "" {
		t.Fatalf("unexpected save notice: %q", m.notice)
	}
	if cmd := m.updateKey(
		"right",
	); cmd != nil || m.config.CoreUpdateChannel != "preview" ||
		m.busy {
		t.Fatal("selecting Preview again should do nothing")
	}
	runCommands(m, m.updateKey("left"))
	if m.config.CoreUpdateChannel != "stable" || f.channel != update.Stable {
		t.Fatal("left should restore Stable")
	}
}

type updateRunningBackend struct{ idleBackend }

func (updateRunningBackend) Snapshot() session.Snapshot {
	return session.Snapshot{State: session.Running}
}
func (updateRunningBackend) Start(context.Context, engine.Options) error { return nil }

func TestUpdaterKeepsActiveSessionAndRecoversFromFailure(t *testing.T) {
	m, f := updaterModel(t)
	press(m, 'u')
	runCommands(m, m.checkUpdate(true))
	m.backend = updateRunningBackend{}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.confirmation != "update" || f.installs != 0 || m.bad {
		t.Fatal("connected download should require confirmation")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "choose whether to restart") {
		t.Fatal("confirmation did not explain restart")
	}
	f.err = errors.New("checksum mismatch")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCommands(m, cmd)
	if !m.bad || m.busy || m.updates.current != "v1.0.0" || m.updates.latest == nil {
		t.Fatal("failed update lost retry state")
	}
	f.err = nil
	press(m, 'u')
	if m.confirmation != "update" {
		t.Fatal("cannot retry failed update")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCommands(m, cmd)
	if !m.running() || !m.updates.restartPending || f.installs != 2 ||
		!strings.Contains(m.notice, "restart") {
		t.Fatal("live update did not preserve connection and request restart")
	}
	m.backend = idleBackend{}
	m.Update(sessionMsg{})
	if m.updates.restartPending {
		t.Fatal("restart notice retained after old session stopped")
	}
}

func TestUpdaterFitsMinimumTerminalAndSanitizes(t *testing.T) {
	m, _ := updaterModel(t)
	press(m, 'u')
	runCommands(m, m.checkUpdate(true))
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"Stable", "Preview", "v1.0.0", "v1.1.0", "Enter", "r check"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q at %v:\n%s", want, size, view)
			}
		}
		press(m, 'u')
		view = ansi.Strip(m.View().Content)
		if !strings.Contains(view, "Confirm") || !strings.Contains(view, "v1.1.0") {
			t.Fatal(view)
		}
		press(m, 'n')
	}
	m.updates.status = "failed\x1b]52;c;SECRET\x07"
	if strings.Contains(m.View().Content, "SECRET") {
		t.Fatal("unsanitized updater output")
	}
}

func TestQuitCancelsUpdateCheckAndQueuedInstall(t *testing.T) {
	m, f := updaterModel(t)
	f.started = make(chan struct{})
	cmd := m.checkUpdate(false)
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	<-f.started
	m.quit()
	msg := <-result
	if f.ctx.Err() == nil {
		t.Fatal("quit left background request active")
	}
	m.Update(msg)
	if m.updates.latest != nil {
		t.Fatal("late result accepted after quit")
	}
	m.workers.close()
	m.updates.latest = &f.releases[0]
	queued := m.installUpdate()
	if queued != nil {
		queued()
	}
	if f.installs != 0 {
		t.Fatal("install ran after shutdown")
	}
}

func TestStartupChecksWithoutDownloading(t *testing.T) {
	m, f := updaterModel(t)
	m.config.EnginePath = "missing-xray-for-test"
	cmd := m.Init()
	if f.checks != 0 || f.installs != 0 {
		t.Fatal("Init performed blocking I/O")
	}
	runCommands(m, cmd)
	if f.checks != 1 || f.installs != 0 || m.updates.latest == nil {
		t.Fatal("startup did not check or downloaded without confirmation")
	}
}

func TestChangedCoreIgnoresOldUpdateResult(t *testing.T) {
	m, _ := updaterModel(t)
	old := m.checkUpdate(false)()
	c := m.config
	c.EnginePath = "another-core"
	_, next := m.Update(actionMsg{config: &c})
	m.Update(old)
	if m.updates.latest != nil || !m.updates.checking {
		t.Fatal("old core result changed candidate")
	}
	runCommands(m, next)
	if m.updates.binary != "another-core" {
		t.Fatal("new core did not get checked")
	}
}

func TestOldVersionProbeCannotOverwriteUpdatedCore(t *testing.T) {
	m, _ := updaterModel(t)
	old := engineVersionMsg{
		binary:  m.config.EnginePath,
		version: "Xray 1.0.0",
		seq:     m.coreVersionSeq,
	}
	m.updateInstalled(
		updateInstalledMsg{result: update.Result{Version: "v1.1.0", VersionOutput: "Xray 1.1.0"}},
	)
	m.Update(old)
	if m.version != "Xray 1.1.0" {
		t.Fatal("late version probe overwrote updated core")
	}
}
