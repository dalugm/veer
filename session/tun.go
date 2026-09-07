package session

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Capture interfaces before launching Xray so another existing VPN cannot
// satisfy the unnamed-TUN readiness check.
func prepareTUN(name string) (func(context.Context) error, error) {
	before, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	return func(ctx context.Context) error {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, err := net.Interfaces()
			if err != nil {
				return fmt.Errorf("list TUN interfaces: %w", err)
			}
			if tunReady(before, current, name) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}, nil
}

func tunReady(before, current []net.Interface, name string) bool {
	for _, nic := range current {
		if nic.Flags&net.FlagUp == 0 {
			continue
		}
		if name != "" && nic.Name != name {
			continue
		}
		if !strings.HasPrefix(nic.Name, "utun") && !strings.HasPrefix(nic.Name, "tun") &&
			nic.Flags&net.FlagPointToPoint == 0 {
			continue
		}
		existed := false
		for _, old := range before {
			if old.Index == nic.Index && old.Name == nic.Name {
				existed = true
				break
			}
		}
		if !existed {
			return true
		}
	}
	return false
}
