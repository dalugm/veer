// Package session owns an engine process and its observable lifecycle.
package session

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

var errCleanup = network.ErrRestore

// State identifies a stage of the managed core lifecycle.
type State string

// Session lifecycle states.
const (
	Stopped    State = "stopped"
	Validating State = "validating"
	Starting   State = "starting"
	Running    State = "running"
	Stopping   State = "stopping"
	Failed     State = "failed"
)

// Snapshot contains a copy of the current state and logs.
type Snapshot struct {
	State          State
	PID            int
	Since          time.Time
	Error          string
	Logs           []string
	Ready          bool
	Endpoints      []string
	Traffic        engine.Traffic
	TrafficError   string
	CleanupPending bool
}

// Adapter prepares core-specific commands for a session.
type Adapter interface {
	Prepare(engine.Options) (engine.Plan, error)
}

// Controller owns one core process and its cleanup operations.
type Controller struct {
	grantLogRead  func(context.Context, *os.File) error
	platform      string
	cleanupErr    error
	cleanup       func(context.Context) error
	recovering    chan struct{}
	prepareDNS    func(context.Context, []string, string) (network.DNSChange, error)
	prepareProxy  func(context.Context, string, string) (network.ProxyChange, error)
	prepareTUN    func(string) (func(context.Context) error, error)
	sampleTraffic func(context.Context, string, string) (engine.Traffic, error)
	mu            sync.Mutex
	adapter       Adapter
	snapshot      Snapshot
	cancel        context.CancelFunc
	done          chan struct{}
}

// New creates an idle controller using the given core adapter.
func New(a Adapter, options ...Option) *Controller {
	c := &Controller{
		platform:      runtime.GOOS,
		prepareDNS:    network.PrepareDNS,
		prepareProxy:  network.PrepareProxy,
		prepareTUN:    prepareTUN,
		sampleTraffic: engine.SampleTraffic,
		adapter:       a,
		snapshot:      Snapshot{State: Stopped},
	}
	for _, option := range options {
		option(c)
	}
	return c
}

// Option configures an optional OS boundary before a controller starts.
type Option func(*Controller)

// WithLogAccess grants the originating account read access when a log is opened.
// It runs outside the terminal loop, including when log files are rotated.
func WithLogAccess(grant func(context.Context, *os.File) error) Option {
	return func(c *Controller) { c.grantLogRead = grant }
}

// Snapshot returns a copy of the current state and logs.
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot
	s.Logs = append([]string(nil), s.Logs...)
	s.Endpoints = append([]string(nil), s.Endpoints...)
	return s
}

func (c *Controller) finish(ctx context.Context, err error, done chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.PID = 0
	c.snapshot.Ready = false
	if errors.Is(err, errCleanup) {
		c.cleanupErr = err
		c.snapshot.CleanupPending = true
	} else {
		c.cleanup = nil
	}
	if ctx.Err() != nil && !errors.Is(err, errCleanup) {
		c.snapshot.State = Stopped
	} else {
		c.snapshot.State = Failed
		if err == nil {
			err = errors.New("engine exited unexpectedly")
		}
		c.snapshot.Error = err.Error()
	}
	c.cancel = nil
	close(done)
}

// Stop cancels the session and waits for process and network cleanup.
func (c *Controller) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	if cancel == nil {
		c.mu.Unlock()
		return c.retryCleanup(ctx)
	}
	c.snapshot.State = Stopping
	c.mu.Unlock()
	cancel()
	select {
	case <-done:
		if s := c.Snapshot(); s.State == Failed {
			return errors.New(s.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
