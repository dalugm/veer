package session

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

type fakeDNS struct {
	apply   func(context.Context) error
	restore func(context.Context) error
}

func (d fakeDNS) Apply(ctx context.Context) error {
	if d.apply != nil {
		return d.apply(ctx)
	}
	return nil
}

func (d fakeDNS) Restore(ctx context.Context) error {
	if d.restore != nil {
		return d.restore(ctx)
	}
	return nil
}

func TestDNSAppliedOnlyAfterTUNReady(t *testing.T) {
	c := New(fakeEngine{"run"})
	var mu sync.Mutex
	var events []string
	record := func(s string) { mu.Lock(); defer mu.Unlock(); events = append(events, s) }
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) {
		if c.Snapshot().PID != 0 {
			t.Error("DNS snapshot taken after process started")
		}
		record("snapshot")
		return fakeDNS{
			apply:   func(context.Context) error { record("apply"); return nil },
			restore: func(context.Context) error { record("restore"); return nil },
		}, nil
	}
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		record("interfaces")
		return func(context.Context) error {
			if c.Snapshot().PID == 0 {
				t.Error("TUN readiness checked before core started")
			}
			record("ready")
			return nil
		}, nil
	}
	if err := c.Start(t.Context(), engine.Options{DNS: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(events, []string{"snapshot", "interfaces", "ready", "apply", "restore"}) {
		t.Fatalf("DNS lifecycle: %v", events)
	}
}

func TestTUNFailureDoesNotApplyDNS(t *testing.T) {
	c := New(fakeEngine{"run"})
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) {
		return fakeDNS{
			apply: func(context.Context) error { t.Error("DNS applied without ready TUN"); return nil },
		}, nil
	}
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		return func(context.Context) error { return errors.New("TUN not ready") }, nil
	}
	if err := c.Start(t.Context(), engine.Options{DNS: []string{"1.1.1.1"}}); err == nil {
		t.Fatal("accepted failed TUN readiness")
	}
	if s := c.Snapshot(); s.PID != 0 || s.State != Failed {
		t.Fatalf("core left running: %+v", s)
	}
}

func TestDNSApplyFailureStopsCoreAndRestores(t *testing.T) {
	c := New(fakeEngine{"run"})
	restored := make(chan struct{}, 1)
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) {
		return fakeDNS{
			apply:   func(context.Context) error { return errors.New("DNS denied") },
			restore: func(context.Context) error { restored <- struct{}{}; return nil },
		}, nil
	}
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	if err := c.Start(t.Context(), engine.Options{DNS: []string{"1.1.1.1"}}); err == nil {
		t.Fatal("accepted failed DNS apply")
	}
	if s := c.Snapshot(); s.PID != 0 || s.State != Failed {
		t.Fatalf("core left running: %+v", s)
	}
	select {
	case <-restored:
	default:
		t.Fatal("DNS was not restored")
	}
}

type trafficEngine struct{ fakeEngine }

func (e trafficEngine) Prepare(o engine.Options) (engine.Plan, error) {
	p, err := e.fakeEngine.Prepare(o)
	p.StatsAddress = "127.0.0.1:1"
	return p, err
}

func TestTrafficSamplerStopsBeforeSessionFinishes(t *testing.T) {
	c := New(trafficEngine{fakeEngine{"run"}})
	entered, finished := make(chan struct{}), make(chan struct{})
	c.sampleTraffic = func(ctx context.Context, _, _ string) (engine.Traffic, error) {
		close(entered)
		<-ctx.Done()
		close(finished)
		return engine.Traffic{}, ctx.Err()
	}
	if err := c.Start(t.Context(), engine.Options{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("sampler did not start")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("sampler outlived Stop")
	}
}
