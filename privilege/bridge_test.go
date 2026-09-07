package privilege

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

func TestBridgeRejectsWrongToken(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	go func() {
		if err := json.NewEncoder(b).Encode("wrong"); err != nil {
			t.Error(err)
		}
	}()
	if _, err := authenticate(a, "right"); err == nil {
		t.Fatal("accepted wrong token")
	}
}

func TestHelperRefusesNonLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := Serve(ctx, "192.0.2.1:4567", "token"); err == nil {
		t.Fatal("non-loopback helper accepted")
	}
}

func TestCancelledRemoteReturnsToStopped(t *testing.T) {
	started := make(chan struct{})
	r := &remote{
		launch: func(string, string, string) (<-chan error, error) { close(started); return nil, nil },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- r.Start(ctx, engine.Options{}) }()
	select {
	case <-started:
	case err := <-result:
		t.Fatalf("helper startup failed before launch: %v", err)
	case <-ctx.Done():
		t.Fatal("helper launch timed out")
	}
	cancel()
	<-result
	<-r.done
	if s := r.Snapshot(); s.State != session.Stopped || s.Ready || s.PID != 0 {
		t.Fatalf("cancelled remote stuck: %#v", s)
	}
}

func TestLauncherFailureReportedPromptly(t *testing.T) {
	r := &remote{launch: func(string, string, string) (<-chan error, error) {
		ch := make(chan error, 1)
		ch <- errors.New("authorization expired")
		return ch, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := r.Start(ctx, engine.Options{})
	if err == nil || !strings.Contains(err.Error(), "authorization expired") {
		t.Fatalf("lost launcher error: %v", err)
	}
}

func TestCancellationWaitsForHelperCleanupResult(t *testing.T) {
	stopped := make(chan error, 1)
	release := make(chan struct{})
	r := &remote{launch: func(_ string, addr, token string) (<-chan error, error) {
		go func() {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				stopped <- err
				return
			}
			defer func() { _ = conn.Close() }()
			enc := json.NewEncoder(conn)
			dec := json.NewDecoder(conn)
			if err := enc.Encode(token); err != nil {
				t.Error(err)
				return
			}
			var o engine.Options
			if err := dec.Decode(&o); err != nil {
				stopped <- err
				return
			}
			if err := enc.Encode(session.Snapshot{State: session.Running, PID: 123}); err != nil {
				t.Error(err)
				return
			}
			var command string
			err = dec.Decode(&command)
			stopped <- err
			if err != nil {
				return
			}
			<-release
			if err := enc.Encode(
				session.Snapshot{State: session.Failed, Error: "DNS restoration failed: denied"},
			); err != nil {
				t.Error(err)
				return
			}
		}()
		return nil, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := r.Start(ctx, engine.Options{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("closed IPC before cleanup: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no graceful stop request")
	}
	select {
	case <-r.done:
		t.Fatal("returned before helper cleanup")
	default:
	}
	close(release)
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup response not consumed")
	}
	if got := r.Snapshot(); got.State != session.Failed ||
		!strings.Contains(got.Error, "DNS restoration") {
		t.Fatalf("lost restoration failure: %#v", got)
	}
	if err := r.Stop(t.Context()); err == nil {
		t.Fatal("subsequent stop lost cleanup failure")
	}
}
