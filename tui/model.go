package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/geofile"
	"github.com/dalugm/veer/privilege"
	"github.com/dalugm/veer/session"
	"github.com/dalugm/veer/settings"
)

// Page identifies a top-level screen.
type Page int

// Top-level pages appear in navigation order.
const (
	Overview Page = iota
	Profiles
	Logs
	Tools
	Settings
)

var pageNames = []string{"Overview", "Profiles", "Logs", "Tools", "Settings"}

// Model owns terminal navigation, forms and asynchronous session updates.
type Model struct {
	geoSeq                           uint64
	geoDir                           string
	geoFiles                         []geofile.FileInfo
	pathReading                      bool
	pathCancel                       context.CancelFunc
	readPaths                        func(context.Context, string, bool) ([]string, error)
	workers                          jobs
	ctx                              context.Context
	path                             string
	config                           settings.Config
	backend                          privilege.Backend
	snapshot                         session.Snapshot
	page                             Page
	width, height, cursor, logOffset int
	form                             *form
	qr                               *qrModal
	auth                             *authorization
	checkAuthorization               func(context.Context) (bool, error)
	authorize                        func(context.Context, []byte) error
	confirmation                     string
	notice                           string
	bad, busy, quitting              bool
	pendingG                         bool
	showHelp                         bool
	showDetails                      bool
	detailOffset                     int
	profileFilter                    string
	search                           *profileSearch
	busyLabel                        string
	cancelWork                       context.CancelFunc
	version                          string
	info                             engine.Info
	phase                            int
	traffic                          trafficHistory
	now                              time.Time
}
type (
	tickMsg   time.Time
	actionMsg struct {
		geoDir    string
		err       error
		notice    string
		config    *settings.Config
		closeForm bool
		info      engine.Info
		version   string
	}
)

type sessionMsg struct {
	err  error
	quit bool
}

type (
	engineVersionMsg struct{ binary, version string }
	prepareMsg       struct {
		options engine.Options
		need    bool
		err     error
	}
)

// New loads preferences and creates the terminal model.
func New(ctx context.Context, path string, backend privilege.Backend) (*Model, error) {
	c, err := settings.Load(path)
	if err != nil {
		return nil, err
	}
	m := &Model{
		ctx:                ctx,
		checkAuthorization: privilege.AuthorizationCached,
		readPaths:          completePaths,
		authorize:          privilege.Authorize,
		path:               path,
		config:             c,
		backend:            backend,
		width:              100,
		height:             32,
		snapshot:           backend.Snapshot(),
		notice:             "Choose a profile. Find your way.",
		version:            "Checking…",
	}
	m.refreshInfo()
	return m, nil
}

// Init starts periodic session updates and a background core-version query.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.loadVersion(), m.loadGeoInfo(m.geoDirectory()))
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles terminal input and asynchronous results.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		if result, ok := message.(sessionMsg); ok && result.quit {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg := message.(type) {
	case geoInfoMsg:
		if msg.seq == m.geoSeq && msg.dir == m.geoDir {
			m.geoFiles = msg.files
		}
		return m, nil
	case pathQueryMsg:
		return m, m.readPath(msg)
	case pathResultMsg:
		return m, m.pathResult(msg)
	case qrMsg:
		m.busy = false
		m.cancelWork = nil
		m.bad = msg.err != nil
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.qr = msg.modal
			m.notice = ""
		}
		return m, nil
	case engineVersionMsg:
		if msg.binary == m.config.EnginePath {
			m.version = msg.version
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.auth != nil {
			m.auth.input.SetWidth(max(8, min(32, msg.Width-22)))
		}
		if m.search != nil {
			m.search.input.SetWidth(max(12, msg.Width-8))
		}
		if m.form != nil {
			m.form.resize(msg.Width)
		}
		return m, nil
	case tickMsg:
		m.phase++
		m.snapshot = m.backend.Snapshot()
		m.observeTraffic(time.Time(msg))
		return m, tick()
	case actionMsg:
		var refresh tea.Cmd
		m.busy = false
		m.cancelWork = nil
		m.bad = msg.err != nil
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = msg.notice
			if msg.config != nil {
				changedCore := m.config.EnginePath != msg.config.EnginePath
				m.config = *msg.config
				m.ensureProfileCursor()
				m.info = msg.info
				if changedCore {
					m.version = "Checking…"
					refresh = m.loadVersion()
				}
			}
			if msg.closeForm {
				m.form = nil
			}
			if msg.version != "" {
				m.version = msg.version
			}
		}
		if msg.err == nil {
			if msg.geoDir != "" {
				refresh = tea.Batch(refresh, m.loadGeoInfo(msg.geoDir))
			} else if msg.config != nil {
				refresh = tea.Batch(refresh, m.loadGeoInfo(m.geoDirectory()))
			}
		}
		return m, refresh
	case sessionMsg:
		m.auth = nil
		m.busy = false
		m.cancelWork = nil
		m.snapshot = m.backend.Snapshot()
		m.observeTraffic(time.Now())
		m.bad = msg.err != nil
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else if m.snapshot.State == session.Running {
			m.notice = "Connected. Open Logs to inspect engine output."
		} else {
			m.notice = "Disconnected."
		}
		if msg.quit {
			return m, tea.Quit
		}
		return m, nil
	case prepareMsg:
		m.busy = false
		m.cancelWork = nil
		if msg.err != nil {
			m.bad = true
			m.notice = msg.err.Error()
			return m, nil
		}
		if msg.need {
			return m, m.beginAuthorization(msg.options)
		}
		return m, m.runStart(msg.options)
	case authorizationMsg:
		return m, m.authorizationResult(msg)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		if m.auth != nil {
			return m, m.updateAuthorization(msg)
		}
		if m.showDetails {
			m.detailsKey(msg.String())
			return m, nil
		}
		if m.showHelp {
			switch msg.String() {
			case "esc", "q", "?":
				m.showHelp = false
			}
			return m, nil
		}
		if m.search != nil {
			return m, m.updateSearch(msg)
		}
		if m.qr != nil {
			m.qrKey(msg.String())
			return m, nil
		}
		if m.confirmation != "" {
			m.pendingG = false
			switch msg.String() {
			case "y", "enter":
				choice := m.confirmation
				m.confirmation = ""
				switch choice {
				case "quit":
					return m, m.quit()
				case "remove":
					return m, m.removeProfile()
				}
			case "n", "esc":
				m.confirmation = ""
			}
			return m, nil
		}
		if m.form != nil {
			m.pendingG = false
			return m, m.updateForm(msg)
		}
		if m.busy {
			m.pendingG = false
			if msg.String() == "esc" && m.cancelWork != nil {
				m.cancelWork()
				m.notice = "Cancelling…"
			}
			return m, nil
		}
		previousPage := m.page
		if m.navigate(msg.String()) {
			if m.page == Tools && previousPage != Tools {
				return m, m.loadGeoInfo(m.geoDirectory())
			}
			return m, nil
		}
		switch msg.String() {
		case "i":
			if m.page == Overview || m.page == Profiles {
				m.showDetails = true
				m.detailOffset = 0
			}
		case "/":
			return m, m.startSearch()
		case "esc":
			if m.page == Profiles {
				m.profileFilter = ""
				m.ensureProfileCursor()
			}
		case "y":
			return m, m.openQR()
		case "q":
			if m.running() {
				m.confirmation = "quit"
				return m, nil
			}
			return m, m.quit()
		case "1", "2", "3", "4", "5":
			m.page = Page(msg.String()[0] - '1')
			if m.page == Tools {
				return m, m.loadGeoInfo(m.geoDirectory())
			}
		case "a":
			m.openImport()
		case "e":
			switch m.page {
			case Settings:
				return m, m.openSettings()
			case Profiles:
				m.openRename()
			}
		case "c":
			return m, m.connect()
		case "s":
			return m, m.stop(false)
		case "v":
			return m, m.checkVersion()
		case "g":
			if m.page == Tools {
				return m, m.openGeo()
			}
		case "enter":
			if m.page == Profiles {
				return m, m.selectProfile()
			}
			if m.page == Settings {
				return m, m.openSettings()
			}
			if m.page == Overview {
				return m, m.connect()
			}
		case "d", "delete":
			if m.page == Profiles && m.hasFocusedProfile() {
				if m.running() {
					m.bad = true
					m.notice = "Disconnect before removing a profile."
				} else {
					m.confirmation = "remove"
				}
			}
		case "?":
			m.showHelp = true
		}
	case tea.PasteMsg:
		if m.auth != nil {
			return m, m.updateAuthorization(msg)
		}
		if m.search != nil {
			return m, m.updateSearch(msg)
		}
		if m.form != nil {
			return m, m.updateForm(msg)
		}
	default:
		if m.auth != nil {
			return m, m.updateAuthorization(msg)
		}
		if m.search != nil {
			return m, m.updateSearch(msg)
		}
		if m.form != nil {
			return m, m.updateForm(msg)
		}
	}
	return m, nil
}

func (m *Model) running() bool {
	s := m.backend.Snapshot().State
	return s != session.Stopped && s != session.Failed
}

func (m *Model) refreshInfo() {
	m.info = engine.Info{}
	if p, ok := m.config.Active(); ok {
		m.info, _ = engine.Inspect(p.Path)
	}
}

func (m *Model) begin(label string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.busy = true
	m.busyLabel = label
	m.cancelWork = cancel
	m.bad = false
	m.notice = label
	return ctx, cancel
}

func (m *Model) connect() tea.Cmd {
	p, ok := m.config.Active()
	if !ok {
		m.page = Profiles
		m.openImport()
		return nil
	}
	if p.Engine != "xray" {
		m.bad = true
		m.notice = "This version supports Xray profiles only."
		return nil
	}
	if m.running() {
		m.bad = true
		m.notice = "Disconnect the current session first."
		return nil
	}
	o := engine.Options{
		Binary:         m.config.EnginePath,
		Config:         p.Path,
		GeoDir:         m.config.GeoDir,
		NetworkService: m.config.NetworkService,
	}
	dns, err := m.config.DNSServers()
	if err != nil {
		m.bad = true
		m.notice = err.Error()
		return nil
	}
	if runtime.GOOS == "windows" && m.config.DNSMode != "custom" {
		dns = nil
	}
	o.DNS = dns
	ctx, cancel := m.begin("Inspecting configuration…")
	return m.workers.track(func() tea.Msg {
		defer cancel()
		need, err := privilege.NeedsElevation(o)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return prepareMsg{options: o, need: need, err: err}
	})
}

func (m *Model) runStart(o engine.Options) tea.Cmd {
	ctx, cancel := m.begin("Starting Xray…")
	return m.workers.track(func() tea.Msg {
		err := m.backend.Start(ctx, o)
		if err != nil {
			cancel()
		}
		return sessionMsg{err: err}
	})
}

func (m *Model) stop(quit bool) tea.Cmd {
	m.busy = true
	m.busyLabel = "Stopping Xray…"
	m.notice = m.busyLabel
	return func() tea.Msg {
		if quit {
			m.workers.close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return sessionMsg{err: m.backend.Stop(ctx), quit: quit}
	}
}

func (m *Model) quit() tea.Cmd {
	m.clearPathCompletion()
	m.quitting = true
	if m.auth != nil {
		m.auth.input.SetValue("")
	}
	if m.cancelWork != nil {
		m.cancelWork()
	}
	return m.stop(true)
}

func (m *Model) selectProfile() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	if m.running() {
		m.bad = true
		m.notice = "Disconnect before selecting another profile."
		return nil
	}
	c := m.config
	c.Selected = c.Profiles[m.cursor].ID
	m.begin("Saving selection…")
	return m.save(c, "Profile selected.", false)
}

func (m *Model) removeProfile() tea.Cmd {
	if !m.hasFocusedProfile() {
		return nil
	}
	c := m.config
	c.Profiles = append([]settings.Profile(nil), c.Profiles...)
	id := c.Profiles[m.cursor].ID
	c.Profiles = append(c.Profiles[:m.cursor], c.Profiles[m.cursor+1:]...)
	if c.Selected == id {
		c.Selected = ""
		if len(c.Profiles) > 0 {
			c.Selected = c.Profiles[0].ID
		}
	}
	m.cursor = max(0, min(m.cursor, len(c.Profiles)-1))
	m.begin("Removing profile…")
	return m.save(c, "Profile removed. Original config file retained.", false)
}

func (m *Model) save(c settings.Config, notice string, closeForm bool) tea.Cmd {
	path := m.path
	return m.workers.track(func() tea.Msg {
		err := settings.Save(path, c)
		return actionMsg{
			err:       err,
			notice:    notice,
			config:    &c,
			closeForm: closeForm,
			info:      inspectSelected(c),
		}
	})
}

func (m *Model) checkVersion() tea.Cmd {
	ctx, cancel := m.begin("Checking Xray version…")
	binary := m.config.EnginePath
	return m.workers.track(func() tea.Msg {
		defer cancel()
		v, err := readVersion(ctx, binary)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{version: v, notice: v}
	})
}

func (m *Model) loadVersion() tea.Cmd {
	ctx, binary := m.ctx, m.config.EnginePath
	return m.workers.track(func() tea.Msg {
		v, err := readVersion(ctx, binary)
		if err != nil {
			v = "Unavailable"
		}
		return engineVersionMsg{binary: binary, version: v}
	})
}

func readVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	hideProcess(cmd)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("xray version check failed: %w", err)
	}
	v := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if v == "" {
		return "", errors.New("engine returned no version information")
	}
	return v, nil
}

func inspectSelected(c settings.Config) engine.Info {
	if p, ok := c.Active(); ok {
		info, _ := engine.Inspect(p.Path)
		return info
	}
	return engine.Info{}
}
