// Package privilege keeps the terminal unprivileged while a helper owns TUN.
package privilege

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

// Backend provides session lifecycle operations to the terminal interface.
type Backend interface {
	Start(context.Context, engine.Options) error
	Stop(context.Context) error
	Snapshot() session.Snapshot
}

// Manager selects a local session or an elevated helper for each connection.
type Manager struct {
	mu       sync.Mutex
	current  Backend
	starting bool
}

// New creates a manager with an idle local Xray session.
func New() *Manager { return &Manager{current: session.New(engine.Xray{})} }

// Snapshot returns a copy of the current session state.
func (m *Manager) Snapshot() session.Snapshot {
	m.mu.Lock()
	b := m.current
	m.mu.Unlock()
	return b.Snapshot()
}

// Stop stops the current session and waits for cleanup.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	b := m.current
	m.mu.Unlock()
	return b.Stop(ctx)
}

// NeedsElevation reports whether a configuration needs privileges absent from this process.
func NeedsElevation(o engine.Options) (bool, error) {
	info, err := engine.Inspect(o.Config)
	return info.TUN && !Elevated(), err
}

// Start starts a session with the privileges required by its configuration.
func (m *Manager) Start(ctx context.Context, o engine.Options) error {
	m.mu.Lock()
	snapshot := m.current.Snapshot()
	state := snapshot.State
	if m.starting || (state != session.Stopped && state != session.Failed) {
		m.mu.Unlock()
		return errors.New("stop the current session before connecting")
	}
	if snapshot.CleanupPending {
		m.mu.Unlock()
		return errors.New("restore the previous session before connecting")
	}
	m.starting = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.starting = false; m.mu.Unlock() }()
	elevated, err := NeedsElevation(o)
	if err != nil {
		return err
	}
	var b Backend = session.New(engine.Xray{})
	if elevated {
		plan, err := (engine.Xray{}).Prepare(o)
		if err != nil {
			return err
		}
		o.Binary = plan.Binary
		o.Config = plan.Args[2]
		if o.GeoDir != "" {
			o.GeoDir, err = filepath.Abs(o.GeoDir)
			if err != nil {
				return err
			}
		}
		b = &remote{snapshot: session.Snapshot{State: session.Stopped}}
	}
	m.mu.Lock()
	m.current = b
	m.mu.Unlock()
	return b.Start(ctx, o)
}
