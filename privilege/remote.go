package privilege

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

type remote struct {
	writeMu     sync.Mutex
	stopReplies map[uint64]chan error
	nextStopID  uint64
	launch      func(string, string, string) (<-chan error, error)
	mu          sync.Mutex
	snapshot    session.Snapshot
	conn        net.Conn
	done        chan struct{}
	cancel      context.CancelFunc
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
	r.stopReplies = make(map[uint64]chan error)
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
		select {
		case <-done:
		case err := <-ready:
			if err != nil {
				return err
			}
		}
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return parent.Err()
	}
}

func (r *remote) communicate(ctx context.Context, o engine.Options, ready chan<- error) error {
	logReader, err := currentLogReader()
	if err != nil {
		return fmt.Errorf("identify log reader: %w", err)
	}
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
	if err = json.NewEncoder(conn).
		Encode(helperOptions{Options: o, LogReader: logReader}); err != nil {
		return err
	}
	after := context.AfterFunc(ctx, func() {
		id, _, _ := r.requestStop(conn)
		r.forgetStop(id)
	})
	defer after()
	dec := json.NewDecoder(reader)
	announced := false
	for {
		var update helperUpdate
		if err = dec.Decode(&update); err != nil {
			return fmt.Errorf("administrator helper disconnected: %w", err)
		}
		s := update.Snapshot
		r.mu.Lock()
		r.snapshot = s
		r.mu.Unlock()
		if !announced &&
			(s.State == session.Running || s.State == session.Failed || s.State == session.Stopped || (update.StopResult != nil && update.StopResult.Error != "")) {
			var e error
			if s.State != session.Running {
				if update.StopResult != nil && update.StopResult.Error != "" {
					e = update.StopResult.err()
				} else {
					e = fmt.Errorf("helper startup: %s", s.Error)
				}
			}
			ready <- e
			announced = true
		}
		if result := update.StopResult; result != nil {
			r.mu.Lock()
			reply := r.stopReplies[result.ID]
			delete(r.stopReplies, result.ID)
			r.mu.Unlock()
			if reply != nil {
				reply <- result.err()
			}
		}
		if s.State == session.Stopped {
			return nil
		}
		if s.State == session.Failed && !s.CleanupPending {
			return errors.New(s.Error)
		}
	}
}

func (r *remote) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	conn, done, cancel := r.conn, r.done, r.cancel
	r.mu.Unlock()
	if cancel == nil {
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return nil
	}
	var reply <-chan error
	if conn == nil {
		cancel()
	} else {
		id, result, err := r.requestStop(conn)
		if err != nil {
			return err
		}
		defer r.forgetStop(id)
		reply = result
	}
	select {
	case err := <-reply:
		return err
	case <-done:
		// The reader publishes the matching reply before closing done. Prefer
		// it even when both channels become ready, including timeout errors.
		select {
		case err := <-reply:
			return err
		default:
		}
		if s := r.Snapshot(); s.State == session.Failed {
			return errors.New(s.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *remote) requestStop(conn net.Conn) (uint64, <-chan error, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.mu.Lock()
	r.nextStopID++
	id := r.nextStopID
	reply := make(chan error, 1)
	if r.stopReplies == nil {
		r.stopReplies = make(map[uint64]chan error)
	}
	r.stopReplies[id] = reply
	r.mu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		r.forgetStop(id)
		return id, nil, err
	}
	if err := json.NewEncoder(conn).Encode(helperCommand{ID: id, Action: "stop"}); err != nil {
		r.forgetStop(id)
		return id, nil, err
	}
	return id, reply, nil
}

func (r *remote) forgetStop(id uint64) {
	r.mu.Lock()
	delete(r.stopReplies, id)
	r.mu.Unlock()
}
