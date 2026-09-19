// Package session owns an engine process and its observable lifecycle.
package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
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
	State        State
	PID          int
	Since        time.Time
	Error        string
	Logs         []string
	Ready        bool
	Endpoints    []string
	Traffic      engine.Traffic
	TrafficError string
}

// Adapter prepares core-specific commands for a session.
type Adapter interface {
	Prepare(engine.Options) (engine.Plan, error)
}

// Controller owns one core process and its cleanup operations.
type Controller struct {
	cleanupErr    error
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
func New(a Adapter) *Controller {
	return &Controller{
		prepareDNS:    network.PrepareDNS,
		prepareProxy:  network.PrepareProxy,
		prepareTUN:    prepareTUN,
		sampleTraffic: engine.SampleTraffic,
		adapter:       a,
		snapshot:      Snapshot{State: Stopped},
	}
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

// Log appends sanitized output to the bounded session log.
func (c *Controller) Log(s string) {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
	if len(s) > 4096 {
		s = s[:4096] + "…"
	}
	if s == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.Logs = append(c.snapshot.Logs, s)
	if len(c.snapshot.Logs) > 400 {
		c.snapshot.Logs = append([]string(nil), c.snapshot.Logs[len(c.snapshot.Logs)-400:]...)
	}
}

// Start validates and starts the core, waiting for startup or cancellation.
func (c *Controller) Start(parent context.Context, o engine.Options) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return errors.New("stop the current session before connecting")
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.snapshot.State = Validating
	c.snapshot.Error = ""
	c.cleanupErr = nil
	c.snapshot.Ready = false
	c.snapshot.PID = 0
	c.snapshot.Endpoints = nil
	c.snapshot.Traffic = engine.Traffic{}
	c.snapshot.TrafficError = ""
	c.mu.Unlock()
	restore := network.Restore(func(context.Context) error { return nil })
	var dnsMu sync.Mutex
	var proxyMu sync.Mutex
	var proxy network.ProxyChange
	runtimeCleanup := func() error { return nil }
	cleanup := func() error {
		dnsMu.Lock()
		defer dnsMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := restore(ctx)
		proxyMu.Lock()
		if proxy != nil {
			err = errors.Join(proxy.Restore(ctx), err)
		}
		proxyMu.Unlock()
		if err != nil {
			err = errors.Join(errCleanup, err)
		}
		if e := runtimeCleanup(); e != nil {
			err = errors.Join(err, errCleanup, errors.New("remove temporary Xray configuration"), e)
		}
		return err
	}
	fail := func(err error) error {
		err = errors.Join(err, cleanup())
		c.finish(ctx, err, done)
		cancel()
		return err
	}
	plan, err := c.adapter.Prepare(o)
	if err != nil {
		return fail(err)
	}
	if adapter, ok := c.adapter.(interface {
		PrepareRuntime(engine.Plan) (engine.Plan, func() error, error)
	}); ok {
		var remove func() error
		plan, remove, err = adapter.PrepareRuntime(plan)
		if remove != nil {
			runtimeCleanup = remove
		}
		if err != nil {
			return fail(err)
		}
		if plan.StatsAddress == "" {
			c.mu.Lock()
			c.snapshot.TrafficError = "Traffic statistics unavailable for this API configuration"
			c.mu.Unlock()
		}
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	check := exec.CommandContext(checkCtx, plan.Binary, plan.CheckArgs...)
	check.Dir = plan.Dir
	check.Env = append(os.Environ(), plan.Env...)
	configureProcess(check)
	checkLog := &logWriter{controller: c}
	check.Stdout = checkLog
	check.Stderr = checkLog
	err = check.Run()
	checkLog.Flush()
	checkCancel()
	if err != nil {
		return fail(fmt.Errorf("xray configuration validation failed: %w (see Logs)", err))
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	var dns network.DNSChange
	var waitTUN func(context.Context) error
	if plan.Info.TUN && len(o.DNS) > 0 {
		dns, err = c.prepareDNS(ctx, o.DNS, o.NetworkService)
		if err != nil {
			return fail(err)
		}
		restore = dns.Restore
		waitTUN, err = c.prepareTUN(plan.Info.TUNName)
		if err != nil {
			return fail(err)
		}
	}
	if !plan.Info.TUN && plan.Info.ProxyEndpoint != "" {
		proxy, err = c.prepareProxy(ctx, plan.Info.ProxyEndpoint, o.NetworkService)
		if err != nil {
			return fail(fmt.Errorf("prepare system proxy: %w", err))
		}
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	cmd := exec.Command(plan.Binary, plan.Args...)
	cmd.Dir = plan.Dir
	cmd.Env = append(os.Environ(), plan.Env...)
	configureProcess(cmd)
	output := &logWriter{controller: c}
	cmd.Stdout = output
	cmd.Stderr = output
	if err = cmd.Start(); err != nil {
		return fail(fmt.Errorf("start Xray: %w", err))
	}
	c.mu.Lock()
	c.snapshot.State = Starting
	c.snapshot.PID = cmd.Process.Pid
	c.snapshot.Since = time.Now()
	c.snapshot.Endpoints = append([]string(nil), plan.Info.Endpoints...)
	c.mu.Unlock()
	c.Log(fmt.Sprintf("Xray started · PID %d", cmd.Process.Pid))
	samplesDone := make(chan struct{})
	go func() { defer close(samplesDone); c.pollTraffic(ctx, plan) }()
	go func() {
		err := cmd.Wait()
		// Cancel polling/readiness before cleanup. The Wait result still determines
		// whether an unsolicited engine exit is a failure.
		unexpected := ctx.Err() == nil
		cancel()
		<-samplesDone
		output.Flush()
		err = errors.Join(err, cleanup())
		finishCtx := ctx
		if unexpected {
			finishCtx = context.Background()
		}
		c.finish(finishCtx, err, done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = interrupt(cmd)
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				_ = cmd.Process.Kill()
			}
		case <-done:
		}
	}()
	readyCtx, readyCancel := context.WithTimeout(ctx, 15*time.Second)
	defer readyCancel()
	startFailed := func(err error) error {
		cancel()
		<-done
		c.mu.Lock()
		defer c.mu.Unlock()
		// A caller may reconnect after Stop returns while this Start is still
		// unwinding. Never publish the old failure into the new session.
		if c.done != done {
			return err
		}
		err = errors.Join(err, c.cleanupErr)
		if parent.Err() == nil || c.cleanupErr != nil {
			c.snapshot.State = Failed
			c.snapshot.Error = err.Error()
		}
		return err
	}
	if waitTUN != nil {
		if err := waitTUN(readyCtx); err != nil {
			return startFailed(fmt.Errorf("wait for TUN: %w", err))
		}
		dnsMu.Lock()
		err = readyCtx.Err()
		if err == nil {
			err = dns.Apply(readyCtx)
		}
		dnsMu.Unlock()
		if err != nil {
			return startFailed(fmt.Errorf("configure TUN DNS: %w", err))
		}
		c.Log("TUN is ready; system DNS applied (restored on disconnect)")
	}
	started := time.Now()
	for {
		select {
		case <-done:
			s := c.Snapshot()
			if s.Error != "" {
				return errors.New(s.Error)
			}
			return errors.New("xray exited during startup")
		default:
		}
		ready := len(plan.Info.Endpoints) > 0
		for _, address := range plan.Info.Endpoints {
			conn, e := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(
				readyCtx,
				"tcp",
				address,
			)
			if e != nil {
				ready = false
				break
			}
			_ = conn.Close()
		}
		if ready || (len(plan.Info.Endpoints) == 0 && time.Since(started) >= 400*time.Millisecond) {
			proxyMu.Lock()
			if proxy != nil {
				err = proxy.Apply(readyCtx)
			}
			proxyMu.Unlock()
			if err != nil {
				return startFailed(fmt.Errorf("configure system proxy: %w", err))
			}
			if proxy != nil {
				c.Log("System SOCKS proxy applied (restored on disconnect)")
			}
			c.mu.Lock()
			if c.done == done && c.snapshot.State == Starting {
				c.snapshot.State = Running
				c.snapshot.Ready = ready
				c.mu.Unlock()
				if ready {
					c.Log("Local proxy listener is ready")
				} else {
					c.Log("Process running; this config has no probeable local proxy listener")
				}
				return nil
			}
			c.mu.Unlock()
		}
		select {
		case <-done:
			continue
		case <-readyCtx.Done():
			err := readyCtx.Err()
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("local proxy listener did not become ready within 15 seconds")
			}
			return startFailed(err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *Controller) finish(ctx context.Context, err error, done chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.PID = 0
	c.snapshot.Ready = false
	if errors.Is(err, errCleanup) {
		c.cleanupErr = err
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
		err := c.cleanupErr
		c.mu.Unlock()
		return err
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

type logWriter struct {
	mu         sync.Mutex
	controller *Controller
	pending    string
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		idx := strings.IndexByte(w.pending, '\n')
		if idx < 0 {
			break
		}
		w.controller.Log(w.pending[:idx])
		w.pending = w.pending[idx+1:]
	}
	if len(w.pending) > 8192 {
		w.controller.Log(w.pending[:4096])
		w.pending = ""
	}
	return len(p), nil
}

func (w *logWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.controller.Log(w.pending)
	w.pending = ""
}
