// Package network applies optional system DNS overrides with explicit rollback.
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrRestore identifies a failure to restore the previous DNS settings.
var ErrRestore = errors.New("DNS restoration failed")

// Restore restores the DNS settings captured before an override.
type (
	Restore func(context.Context) error
	runner  func(context.Context, string, ...string) (string, error)
)

// DNSChange is a prepared DNS snapshot. Apply and Restore must be called serially.
type DNSChange interface {
	Apply(context.Context) error
	Restore(context.Context) error
}

type dnsChange struct {
	set     func(context.Context, []string) error
	restore Restore
	servers []string
	applied bool
}

func (d *dnsChange) Apply(ctx context.Context) error {
	if d.applied || d.set == nil {
		return nil
	}
	d.applied = true // A failed command may already have changed the host.
	if err := d.set(ctx, d.servers); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return errors.Join(err, d.Restore(cleanup))
	}
	return nil
}

func (d *dnsChange) Restore(ctx context.Context) error {
	if !d.applied {
		return nil
	}
	if err := d.restore(ctx); err != nil {
		return errors.Join(ErrRestore, err)
	}
	d.applied = false
	return nil
}

// PrepareDNS discovers the original network and snapshots DNS without changing it.
func PrepareDNS(ctx context.Context, servers []string, service string) (DNSChange, error) {
	return prepareDNS(ctx, runtime.GOOS, servers, service, run)
}

// ApplyDNS applies an optional DNS override and returns its rollback operation.
func ApplyDNS(ctx context.Context, servers []string, service string) (Restore, error) {
	return applyDNS(ctx, runtime.GOOS, servers, service, run)
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func applyDNS(
	ctx context.Context,
	platform string,
	servers []string,
	service string,
	run runner,
) (Restore, error) {
	change, err := prepareDNS(ctx, platform, servers, service, run)
	if err != nil {
		return nil, err
	}
	if err := change.Apply(ctx); err != nil {
		return nil, err
	}
	return change.Restore, nil
}

func prepareDNS(
	ctx context.Context,
	platform string,
	servers []string,
	service string,
	run runner,
) (DNSChange, error) {
	if len(servers) == 0 {
		return &dnsChange{}, nil
	}
	for _, ip := range servers {
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid DNS server %q", ip)
		}
	}
	var set func(context.Context, []string) error
	var restore Restore
	switch platform {
	case "darwin":
		if service == "" {
			var err error
			service, err = discoverMacService(ctx, run)
			if err != nil {
				return nil, err
			}
		}
		original, err := run(ctx, "networksetup", "-getdnsservers", service)
		if err != nil {
			return nil, err
		}
		previous := strings.Fields(original)
		if strings.HasPrefix(strings.TrimSpace(original), "There aren't any DNS Servers set on ") {
			previous = []string{"Empty"}
		} else if len(previous) == 0 || !validIPs(previous) {
			return nil, errors.New("cannot parse current networksetup DNS settings")
		}
		set = func(ctx context.Context, ips []string) error {
			_, err := run(
				ctx,
				"networksetup",
				append([]string{"-setdnsservers", service}, ips...)...)
			return err
		}
		restore = func(ctx context.Context) error { return set(ctx, previous) }
	case "linux":
		routes, err := run(ctx, "ip", "-o", "route", "show", "default")
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(strings.SplitN(routes, "\n", 2)[0])
		iface := ""
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "dev" {
				iface = fields[i+1]
				break
			}
		}
		if iface == "" {
			return nil, errors.New("cannot determine the default network interface")
		}
		original, err := run(ctx, "resolvectl", "dns", iface)
		if err != nil {
			return nil, err
		}
		_, after, ok := strings.Cut(original, ":")
		if !ok {
			return nil, errors.New("cannot parse current resolvectl DNS settings")
		}
		previous := strings.Fields(after)
		if !strings.HasPrefix(strings.TrimSpace(original), "Link ") ||
			!strings.Contains(strings.SplitN(original, ":", 2)[0], "("+iface+")") ||
			!validIPs(previous) {
			return nil, errors.New("cannot parse current resolvectl DNS settings")
		}
		set = func(ctx context.Context, ips []string) error {
			_, err := run(ctx, "resolvectl", append([]string{"dns", iface}, ips...)...)
			return err
		}
		restore = func(ctx context.Context) error {
			if len(previous) == 0 {
				_, err := run(ctx, "resolvectl", "dns", iface, "")
				return err
			}
			return set(ctx, previous)
		}
	case "windows":
		return nil, errors.New(
			"configure Windows TUN DNS in the Xray JSON; choose auto or off in Settings",
		)
	default:
		return nil, fmt.Errorf("DNS override is not supported on %s", platform)
	}
	return &dnsChange{set: set, restore: restore, servers: append([]string(nil), servers...)}, nil
}

func validIPs(servers []string) bool {
	for _, server := range servers {
		if net.ParseIP(server) == nil {
			return false
		}
	}
	return true
}

func discoverMacService(ctx context.Context, run runner) (string, error) {
	route, err := run(ctx, "route", "-n", "get", "default")
	if err != nil {
		return "", err
	}
	device := ""
	for line := range strings.SplitSeq(route, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && key == "interface" {
			device = strings.TrimSpace(value)
		}
	}
	if device == "" {
		return "", errors.New("cannot determine the default network interface")
	}
	order, err := run(ctx, "networksetup", "-listnetworkserviceorder")
	if err != nil {
		return "", err
	}
	service := ""
	for line := range strings.SplitSeq(order, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "(Hardware Port:") {
			_, current, ok := strings.Cut(line, ", Device: ")
			if ok && strings.TrimSuffix(current, ")") == device && service != "" {
				return service, nil
			}
			service = ""
			continue
		}
		service = ""
		if strings.HasPrefix(line, "(") {
			prefix, name, ok := strings.Cut(line, ") ")
			if ok && prefix != "(*)" && !strings.Contains(prefix, "*") {
				service = strings.TrimSpace(name)
			}
		}
	}
	return "", fmt.Errorf("cannot find an enabled network service for %s", device)
}
