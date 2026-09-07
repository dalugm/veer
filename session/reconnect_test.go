package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

func TestOldStartupFailureDoesNotOverwriteReconnect(t *testing.T) {
	c := New(fakeEngine{"run"})
	c.prepareDNS = func(context.Context, []string, string) (network.DNSChange, error) { return fakeDNS{}, nil }
	enteredFirst, enteredSecond := make(chan struct{}), make(chan struct{})
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	var call atomic.Int32
	c.prepareTUN = func(string) (func(context.Context) error, error) {
		if call.Add(1) == 1 {
			return func(context.Context) error { close(enteredFirst); <-releaseFirst; return nil }, nil
		}
		return func(context.Context) error { close(enteredSecond); <-releaseSecond; return nil }, nil
	}
	opts := engine.Options{DNS: []string{"1.1.1.1"}}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- c.Start(t.Context(), opts) }()
	<-enteredFirst
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	go func() { second <- c.Start(t.Context(), opts) }()
	<-enteredSecond
	close(releaseFirst)
	if err := <-first; err == nil {
		t.Fatal("canceled first startup succeeded")
	}
	if s := c.Snapshot(); s.State != Starting || s.Error != "" {
		t.Fatalf("old startup overwrote reconnect: %+v", s)
	}
	close(releaseSecond)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}
