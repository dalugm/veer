// Package privilege keeps the terminal unprivileged while a helper owns TUN.
package privilege

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	state := m.current.Snapshot().State
	if m.starting || (state != session.Stopped && state != session.Failed) {
		m.mu.Unlock()
		return errors.New("stop the current session before connecting")
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

type remote struct {
	stopOnce sync.Once
	launch   func(string, string, string) (<-chan error, error)
	mu       sync.Mutex
	snapshot session.Snapshot
	conn     net.Conn
	done     chan struct{}
	cancel   context.CancelFunc
}

func (r *remote) Snapshot() session.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.snapshot
	s.Logs = append([]string(nil), s.Logs...)
	return s
}

func (r *remote) Start(parent context.Context, o engine.Options) error {
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.cancel = cancel
	r.done = make(chan struct{})
	r.snapshot = session.Snapshot{State: session.Starting}
	done := r.done
	r.mu.Unlock()
	ready := make(chan error, 1)
	go func() {
		err := r.communicate(ctx, o, ready)
		r.mu.Lock()
		if ctx.Err() != nil && r.conn == nil {
			r.snapshot.State = session.Stopped
			r.snapshot.Error = ""
		} else if err != nil {
			r.snapshot.State = session.Failed
			r.snapshot.Error = err.Error()
		}
		r.snapshot.PID = 0
		r.snapshot.Ready = false
		r.cancel = nil
		conn := r.conn
		r.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		cancel()
		select {
		case ready <- err:
		default:
		}
		close(done)
	}()
	select {
	case err := <-ready:
		return err
	case <-parent.Done():
		<-done
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return parent.Err()
	}
}

func (r *remote) communicate(ctx context.Context, o engine.Options, ready chan<- error) error {
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes[:])
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	if err := listener.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return err
	}
	stopAccept := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopAccept()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	launch := r.launch
	if launch == nil {
		launch = launchHelper
	}
	launched, err := launch(exe, listener.Addr().String(), token)
	if err != nil {
		return fmt.Errorf("request elevation: %w", err)
	}
	type accepted struct {
		conn   net.Conn
		reader *bufio.Reader
		err    error
	}
	connections := make(chan accepted)
	accepting, stopAccepting := context.WithCancel(ctx)
	defer stopAccepting()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				select {
				case connections <- accepted{err: e}:
				case <-accepting.Done():
				}
				return
			}
			rd, e := authenticate(c, token)
			if e != nil {
				_ = c.Close()
				continue
			}
			select {
			case connections <- accepted{conn: c, reader: rd}:
			case <-accepting.Done():
				_ = c.Close()
			}
			return
		}
	}()
	var conn net.Conn
	var reader *bufio.Reader
	select {
	case accepted := <-connections:
		if accepted.err != nil {
			return fmt.Errorf("waiting for administrator helper: %w", accepted.err)
		}
		conn = accepted.conn
		reader = accepted.reader
	case err := <-launched:
		if err == nil {
			err = errors.New("helper exited before connecting")
		}
		return fmt.Errorf("administrator helper failed: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { _ = conn.Close() }()
	_ = listener.Close()
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()
	if err = json.NewEncoder(conn).Encode(o); err != nil {
		return err
	}
	after := context.AfterFunc(ctx, func() { r.requestStop(conn) })
	defer after()
	dec := json.NewDecoder(reader)
	announced := false
	for {
		var s session.Snapshot
		if err = dec.Decode(&s); err != nil {
			return fmt.Errorf("administrator helper disconnected: %w", err)
		}
		r.mu.Lock()
		r.snapshot = s
		r.mu.Unlock()
		if !announced &&
			(s.State == session.Running || s.State == session.Failed || s.State == session.Stopped) {
			var e error
			if s.State != session.Running {
				e = fmt.Errorf("helper startup: %s", s.Error)
			}
			ready <- e
			announced = true
		}
		if s.State == session.Stopped {
			return nil
		}
		if s.State == session.Failed {
			return errors.New(s.Error)
		}
	}
}

func (r *remote) Stop(ctx context.Context) error {
	r.mu.Lock()
	conn, done, cancel := r.conn, r.done, r.cancel
	r.mu.Unlock()
	if cancel == nil {
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return nil
	}
	if conn == nil {
		cancel()
	} else {
		r.requestStop(conn)
	}
	select {
	case <-done:
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return nil
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
}

func (r *remote) requestStop(conn net.Conn) {
	r.stopOnce.Do(func() {
		if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			_ = conn.Close()
			return
		}
		if err := conn.SetReadDeadline(time.Now().Add(17 * time.Second)); err != nil {
			_ = conn.Close()
			return
		}
		if err := json.NewEncoder(conn).Encode("stop"); err != nil {
			_ = conn.Close()
		}
	})
}

func authenticate(conn net.Conn, token string) (*bufio.Reader, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 1024)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	var got string
	if err = json.Unmarshal(line, &got); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		return nil, errors.New("invalid helper token")
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return reader, nil
}

// Serve is only entered through the private helper dispatch in main.
func Serve(parent context.Context, address, token string) (result error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return errors.New("helper requires a loopback address")
	}
	if len(token) != 64 {
		return errors.New("invalid helper token length")
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(parent, "tcp4", address)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	enc := json.NewEncoder(conn)
	if err = enc.Encode(token); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	var options engine.Options
	if err = dec.Decode(&options); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	controller := session.New(engine.Xray{})
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		result = errors.Join(result, controller.Stop(cleanup))
	}()
	go func() { var command string; _ = dec.Decode(&command); cancel() }()
	started := make(chan error, 1)
	go func() { started <- controller.Start(ctx, options) }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	send := func() error {
		if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return err
		}
		return enc.Encode(controller.Snapshot())
	}
	for {
		select {
		case <-ctx.Done():
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			err := controller.Stop(cleanup)
			stop()
			_ = send()
			return err
		case err := <-started:
			if err != nil {
				_ = send()
				return err
			}
			if err = send(); err != nil {
				return err
			}
		case <-ticker.C:
			s := controller.Snapshot()
			if s.State == session.Stopped {
				continue
			}
			if err = send(); err != nil {
				return err
			}
			if s.State == session.Failed {
				return errors.New(s.Error)
			}
		}
	}
}
