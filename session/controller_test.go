package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

type fakeEngine struct{ mode string }

func (f fakeEngine) Prepare(engine.Options) (engine.Plan, error) {
	exe, _ := os.Executable()
	return engine.Plan{
		Info:      engine.Info{TUN: true},
		Binary:    exe,
		Args:      []string{"-test.run=TestCoreProcess", "--", f.mode},
		CheckArgs: []string{"-test.run=TestCoreProcess", "--", "check"},
		Env:       []string{"VEER_TEST_CORE=1"},
	}, nil
}

func TestCoreProcess(t *testing.T) {
	if os.Getenv("VEER_TEST_CORE") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "check" {
		os.Exit(0)
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "bad engine config")
		os.Exit(3)
	}
	fmt.Println("core started")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(4)
	}
	defer func() { _ = l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			os.Exit(0)
		}
		_ = c.Close()
	}
}

func TestControllerOwnsLifecycle(t *testing.T) {
	c := New(fakeEngine{"run"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := c.Start(ctx, engine.Options{}); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot(); got.State != Running || got.PID == 0 {
		t.Fatalf("snapshot: %#v", got)
	}
	if err := c.Start(ctx, engine.Options{}); err == nil {
		t.Fatal("second start accepted")
	}
	stopCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if err := c.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot(); got.State != Stopped || got.PID != 0 ||
		!strings.Contains(strings.Join(got.Logs, "\n"), "core started") {
		t.Fatalf("stopped: %#v", got)
	}
}

func TestEarlyExitIsFailure(t *testing.T) {
	c := New(fakeEngine{"fail"})
	if err := c.Start(t.Context(), engine.Options{}); err == nil {
		t.Fatal("accepted crashed process")
	}
	if s := c.Snapshot(); s.State != Failed {
		t.Fatalf("state: %#v", s)
	}
}

func TestCancelStopsProcess(t *testing.T) {
	c := New(fakeEngine{"run"})
	ctx, cancel := context.WithCancel(t.Context())
	if err := c.Start(ctx, engine.Options{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.After(5 * time.Second)
	for {
		if c.Snapshot().State == Stopped {
			return
		}
		select {
		case <-deadline:
			t.Fatal("process not stopped")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestLogBufferIsBoundedAndSanitized(t *testing.T) {
	c := New(fakeEngine{})
	for i := 0; i < 1000; i++ {
		c.Log(fmt.Sprintf("\x1b[31mline %d\x1b[0m\x00", i))
	}
	s := c.Snapshot()
	if len(s.Logs) > 400 || strings.Contains(strings.Join(s.Logs, ""), "\x1b") {
		t.Fatal("unbounded or unsafe log")
	}
}

func TestDNSIsRestoredBeforeStopReturns(t *testing.T) {
	c := New(fakeEngine{"run"})
	var restored atomic.Bool
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) {
		return fakeDNS{
			restore: func(context.Context) error { restored.Store(true); return nil },
		}, nil
	}
	if err := c.Start(t.Context(), engine.Options{DNS: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !restored.Load() {
		t.Fatal("stop returned before DNS restoration")
	}
}

func TestDNSRestorationFailureIsReported(t *testing.T) {
	c := New(fakeEngine{"run"})
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) {
		return fakeDNS{
			restore: func(context.Context) error { return errors.New("restore denied") },
		}, nil
	}
	if err := c.Start(t.Context(), engine.Options{DNS: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := c.Stop(ctx)
	if err == nil || !strings.Contains(err.Error(), "restore denied") {
		t.Fatalf("lost cleanup failure: %v", err)
	}
	if err := c.Stop(ctx); err == nil {
		t.Fatal("second Stop lost DNS restoration failure")
	}
}
