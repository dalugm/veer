package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/network"
)

// networkState owns this connection's changes. Applying and restoring them
// share a lock so process exit cannot restore settings while startup applies them.
type networkState struct {
	mu      sync.Mutex
	dns     network.DNSChange
	proxy   network.ProxyChange
	waitTUN func(context.Context) error
	coreDNS bool
}

func (c *Controller) prepareNetwork(
	ctx context.Context,
	info engine.Info,
	options engine.Options,
	state *networkState,
) error {
	state.coreDNS = info.TUN && info.TUNSystemDNS && c.platform == "linux"
	if info.TUN && len(options.DNS) > 0 && !state.coreDNS {
		dns, err := c.prepareDNS(ctx, options.DNS, options.NetworkService)
		if err != nil {
			return err
		}
		state.dns = dns
	}
	if state.dns != nil || state.coreDNS {
		wait, err := c.prepareTUN(info.TUNName)
		if err != nil {
			return err
		}
		state.waitTUN = wait
	}
	if !info.TUN && info.ProxyEndpoint != "" {
		proxy, err := c.prepareProxy(ctx, info.ProxyEndpoint, options.NetworkService)
		if err != nil {
			return fmt.Errorf("prepare system proxy: %w", err)
		}
		state.proxy = proxy
	}
	return nil
}

func (s *networkState) readyTUN(ctx context.Context, log func(string)) error {
	if s.waitTUN == nil {
		return nil
	}
	if err := s.waitTUN(ctx); err != nil {
		return fmt.Errorf("wait for TUN: %w", err)
	}
	if s.dns != nil {
		s.mu.Lock()
		err := ctx.Err()
		if err == nil {
			err = s.dns.Apply(ctx)
		}
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("configure TUN DNS: %w", err)
		}
		log("TUN is ready; system DNS applied (restored on disconnect)")
	} else if s.coreDNS {
		log("TUN is ready; system DNS is managed by Xray")
	}
	return nil
}

func (s *networkState) applyProxy(ctx context.Context, log func(string)) error {
	if s.proxy == nil {
		return nil
	}
	s.mu.Lock()
	err := ctx.Err()
	if err == nil {
		err = s.proxy.Apply(ctx)
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("configure system proxy: %w", err)
	}
	log("System SOCKS proxy applied (restored on disconnect)")
	return nil
}

func (s *networkState) restore(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.dns != nil {
		err = s.dns.Restore(ctx)
	}
	if s.proxy != nil {
		err = errors.Join(s.proxy.Restore(ctx), err)
	}
	return err
}
