package session

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

type proxyEngine struct{ info engine.Info }

func (e proxyEngine) Prepare(engine.Options) (engine.Plan, error) {
	exe, _ := os.Executable()
	return engine.Plan{
		Info:      e.info,
		Binary:    exe,
		Args:      []string{"-test.run=TestCoreProcess", "--", "run"},
		CheckArgs: []string{"-test.run=TestCoreProcess", "--", "check"},
		Env:       []string{"VEER_TEST_CORE=1"},
	}, nil
}

func newFakeController(t *testing.T, info engine.Info) *Controller {
	t.Helper()
	c := New(proxyEngine{info: info})
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) { return fakeDNS{}, nil }
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	return c
}

type fakeProxyChange struct {
	applied, restored    int
	applyErr, restoreErr error
}

func (p *fakeProxyChange) Apply(context.Context) error   { p.applied++; return p.applyErr }
func (p *fakeProxyChange) Restore(context.Context) error { p.restored++; return p.restoreErr }

func TestSystemProxyAppliesOnlyWithoutTUNAndRestores(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		info                   engine.Info
		wantPrepare, wantApply bool
	}{
		{"socks", engine.Info{ProxyEndpoint: "127.0.0.1:1080"}, true, true},
		{"tun and socks", engine.Info{TUN: true, ProxyEndpoint: "127.0.0.1:1080"}, false, false},
		{"no proxy", engine.Info{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := newFakeController(t, tc.info)
			proxy := &fakeProxyChange{}
			prepared := false
			backend.prepareProxy = func(context.Context, string, string) (network.ProxyChange, error) { prepared = true; return proxy, nil }
			if err := backend.Start(t.Context(), engine.Options{}); err != nil {
				t.Fatal(err)
			}
			if prepared != tc.wantPrepare || (proxy.applied > 0) != tc.wantApply {
				t.Fatalf("prepare=%v apply=%d", prepared, proxy.applied)
			}
			if err := backend.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if tc.wantApply && proxy.restored != 1 {
				t.Fatalf("restore=%d", proxy.restored)
			}
		})
	}
}

func TestSystemProxyApplyFailureStopsSession(t *testing.T) {
	backend := newFakeController(t, engine.Info{ProxyEndpoint: "127.0.0.1:1080"})
	proxy := &fakeProxyChange{applyErr: errors.New("proxy denied")}
	backend.prepareProxy = func(context.Context, string, string) (network.ProxyChange, error) { return proxy, nil }
	if err := backend.Start(t.Context(), engine.Options{}); err == nil {
		t.Fatal("proxy failure was ignored")
	}
	if proxy.applied != 1 || proxy.restored != 1 {
		t.Fatalf("proxy lifecycle: %+v", proxy)
	}
}
