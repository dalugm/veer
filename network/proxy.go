package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrProxyUnavailable means no supported desktop proxy backend is installed.
// No settings have been changed; callers may still run a local proxy listener.
var ErrProxyUnavailable = errors.New("system proxy integration is unavailable")

// ProxyChange snapshots and restores the user's system proxy settings.
type ProxyChange interface {
	Apply(context.Context) error
	Restore(context.Context) error
}

type proxySnapshot struct {
	platform, service, host, port string
	enabled                       bool
	extra                         map[string]string
	registry                      map[string]registryValue
}

type proxyChange struct {
	snapshot proxySnapshot
	endpoint string
	run      runner
	applied  bool
	notify   func(context.Context) error
	touched  map[string]bool
}

// PrepareProxy captures the current system proxy without changing it.
func PrepareProxy(ctx context.Context, endpoint, service string) (ProxyChange, error) {
	p, err := prepareProxy(ctx, runtime.GOOS, endpoint, service, run)
	if err != nil {
		return nil, err
	}
	p.notify = notifyProxyChanged
	return p, nil
}

func prepareProxy(
	ctx context.Context,
	platform, endpoint, service string,
	run runner,
) (*proxyChange, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || !isLoopbackHost(host) {
		return nil, errors.New("system proxy requires a loopback SOCKS endpoint")
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
		return nil, errors.New("invalid system proxy port")
	}
	s := proxySnapshot{
		platform: platform,
		service:  service,
		host:     host,
		port:     port,
		extra:    map[string]string{},
	}
	switch platform {
	case "darwin":
		if service == "" {
			service, err = discoverMacService(ctx, run)
			if err != nil {
				return nil, fmt.Errorf("discover macOS network service: %w", err)
			}
			s.service = service
		}
		out, err := run(ctx, "networksetup", "-getsocksfirewallproxy", service)
		if err != nil {
			return nil, fmt.Errorf("read macOS SOCKS proxy: %w", err)
		}
		if err := parseMacProxy(&s, out); err != nil {
			return nil, err
		}
	case "windows":
		out, err := run(
			ctx,
			"reg",
			"query",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		)
		if err != nil {
			return nil, fmt.Errorf("read Windows proxy: %w", err)
		}
		parseWindowsProxy(&s, out)
	case "linux":
		out, err := run(ctx, "gsettings", "list-schemas")
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: gsettings is not installed", ErrProxyUnavailable)
		}
		if err != nil {
			return nil, fmt.Errorf("inspect Linux desktop proxy: %w", err)
		}
		schemas := strings.Fields(out)
		for _, setting := range linuxProxySettings {
			if !slices.Contains(schemas, setting.schema) {
				return nil, fmt.Errorf("%w: missing GNOME proxy schema", ErrProxyUnavailable)
			}
		}
		for _, setting := range linuxProxySettings {
			out, err := run(ctx, "gsettings", "get", setting.schema, setting.key)
			if err != nil {
				return nil, fmt.Errorf("read Linux desktop proxy: %w", err)
			}
			s.extra[setting.name()] = trimSetting(out)
		}
	default:
		return nil, fmt.Errorf("unsupported system proxy platform: %s", platform)
	}
	return &proxyChange{
		snapshot: s,
		endpoint: net.JoinHostPort(host, port),
		run:      run,
		touched:  make(map[string]bool),
		notify:   func(context.Context) error { return nil },
	}, nil
}

func (p *proxyChange) Apply(ctx context.Context) error {
	if p.applied {
		return nil
	}
	p.applied = true
	host, port, _ := net.SplitHostPort(p.endpoint)
	var err error
	switch p.snapshot.platform {
	case "darwin":
		err = p.applyDarwin(ctx, host, port)
	case "windows":
		err = p.applyWindows(ctx, host, port)
	case "linux":
		err = p.applyLinux(ctx, host, port)
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return errors.Join(err, p.Restore(cleanup))
	}
	return nil
}

func (p *proxyChange) Restore(ctx context.Context) error {
	if !p.applied {
		return nil
	}
	var err error
	switch p.snapshot.platform {
	case "darwin":
		err = p.restoreDarwin(ctx)
	case "windows":
		err = p.restoreWindows(ctx)
	case "linux":
		err = p.restoreLinux(ctx)
	}
	if err == nil {
		p.applied = false
	}
	return err
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
