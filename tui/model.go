package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/engine"
	update "github.com/dalugm/veer/engine/coreupdate"
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
	updates                          appUpdate
	coreVersionSeq                   uint64
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
	runningVersion                   string
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
	err     error
	quit    bool
	started bool
	version string
}

type restartStoppedMsg struct {
	err error
	ctx context.Context
}

type (
	engineVersionMsg struct {
		binary, version string
		seq             uint64
	}
	prepareMsg struct {
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
		updates: appUpdate{
			client:      update.New(),
			current:     "Checking…",
			readVersion: readVersion,
			status:      "Press r to check.",
		},
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

// Init starts session updates and local core/version queries.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.loadVersion(), m.loadGeoInfo(m.geoDirectory()))
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) running() bool {
	s := m.backend.Snapshot()
	return s.CleanupPending || (s.State != session.Stopped && s.State != session.Failed)
}

func (m *Model) refreshInfo() {
	m.info = inspectSelected(m.config)
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

func inspectSelected(c settings.Config) engine.Info {
	if p, ok := c.Active(); ok {
		info, _ := engine.Inspect(p.Path)
		return info
	}
	return engine.Info{}
}
