package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ProxyChange snapshots and restores the user's system proxy settings.
type ProxyChange interface {
	Apply(context.Context) error
	Restore(context.Context) error
}

type proxyRunner struct{}

func (proxyRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	return run(ctx, name, args...)
}

type proxySnapshot struct {
	platform, service, host, port string
	enabled                       bool
	extra                         map[string]string
}

type proxyChange struct {
	snapshot proxySnapshot
	endpoint string
	run      runner
	applied  bool
}

// PrepareProxy captures the current system proxy without changing it.
func PrepareProxy(ctx context.Context, endpoint, service string) (ProxyChange, error) {
	return prepareProxy(ctx, runtime.GOOS, endpoint, service, proxyRunner{}.run)
}

func prepareProxy(
	ctx context.Context,
	platform, endpoint, service string,
	run runner,
) (ProxyChange, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || !isLoopbackHost(host) {
		return nil, errors.New("system proxy requires a loopback SOCKS endpoint")
	}
	if _, err := strconv.Atoi(port); err != nil {
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
		parseMacProxy(&s, out)
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
		for _, key := range []string{"mode", "socks host", "socks port", "http host", "http port", "https host", "https port", "ignore-hosts"} {
			out, err := run(ctx, "gsettings", "get", "org.gnome.system.proxy", key)
			if err != nil {
				return nil, fmt.Errorf("read Linux desktop proxy: %w", err)
			}
			s.extra[key] = strings.TrimSpace(out)
		}
	default:
		return nil, fmt.Errorf("unsupported system proxy platform: %s", platform)
	}
	return &proxyChange{snapshot: s, endpoint: net.JoinHostPort(host, port), run: run}, nil
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

func parseMacProxy(s *proxySnapshot, output string) {
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Enabled":
			s.enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "Server":
			s.extra["server"] = strings.TrimSpace(value)
		case "Port":
			s.extra["port"] = strings.TrimSpace(value)
		}
	}
}

func parseWindowsProxy(s *proxySnapshot, output string) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		switch fields[0] {
		case "ProxyEnable":
			s.enabled = strings.TrimSpace(fields[2]) == "0x1"
		case "ProxyServer", "ProxyOverride":
			s.extra[fields[0]] = strings.Join(fields[2:], " ")
		}
	}
}

func quoteGSetting(v string) string { return strings.TrimSpace(v) }

func (p *proxyChange) applyLinux(ctx context.Context, host, port string) error {
	for _, item := range [][2]string{{"mode", "'manual'"}, {"socks host", quoteGSetting("'" + host + "'")}, {"socks port", port}} {
		if _, err := p.run(
			ctx,
			"gsettings",
			"set",
			"org.gnome.system.proxy",
			item[0],
			item[1],
		); err != nil {
			return err
		}
	}
	return nil
}

func (p *proxyChange) restoreLinux(ctx context.Context) error {
	for _, key := range []string{"mode", "socks host", "socks port", "http host", "http port", "https host", "https port", "ignore-hosts"} {
		if _, err := p.run(
			ctx,
			"gsettings",
			"set",
			"org.gnome.system.proxy",
			key,
			p.snapshot.extra[key],
		); err != nil {
			return err
		}
	}
	return nil
}

func (p *proxyChange) applyDarwin(ctx context.Context, host, port string) error {
	if _, err := p.run(
		ctx,
		"networksetup",
		"-setsocksfirewallproxy",
		p.snapshot.service,
		host,
		port,
	); err != nil {
		return err
	}
	_, err := p.run(ctx, "networksetup", "-setsocksfirewallproxystate", p.snapshot.service, "on")
	return err
}

func (p *proxyChange) restoreDarwin(ctx context.Context) error {
	if !p.snapshot.enabled {
		_, err := p.run(
			ctx,
			"networksetup",
			"-setsocksfirewallproxystate",
			p.snapshot.service,
			"off",
		)
		return err
	}
	if _, err := p.run(
		ctx,
		"networksetup",
		"-setsocksfirewallproxy",
		p.snapshot.service,
		p.snapshot.extra["server"],
		p.snapshot.extra["port"],
	); err != nil {
		return err
	}
	_, err := p.run(ctx, "networksetup", "-setsocksfirewallproxystate", p.snapshot.service, "on")
	return err
}

func (p *proxyChange) applyWindows(ctx context.Context, host, port string) error {
	server := "socks=" + net.JoinHostPort(host, port)
	if _, err := p.run(
		ctx,
		"reg",
		"add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v",
		"ProxyEnable",
		"/t",
		"REG_DWORD",
		"/d",
		"1",
		"/f",
	); err != nil {
		return err
	}
	_, err := p.run(
		ctx,
		"reg",
		"add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v",
		"ProxyServer",
		"/t",
		"REG_SZ",
		"/d",
		server,
		"/f",
	)
	if err != nil {
		return err
	}
	_, err = p.run(ctx, "rundll32", "user32.dll,UpdatePerUserSystemParameters")
	return err
}

func (p *proxyChange) restoreWindows(ctx context.Context) error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	if _, err := p.run(
		ctx,
		"reg",
		"add",
		key,
		"/v",
		"ProxyEnable",
		"/t",
		"REG_DWORD",
		"/d",
		map[bool]string{true: "1", false: "0"}[p.snapshot.enabled],
		"/f",
	); err != nil {
		return err
	}
	if value := p.snapshot.extra["ProxyServer"]; value != "" {
		if _, err := p.run(
			ctx,
			"reg",
			"add",
			key,
			"/v",
			"ProxyServer",
			"/t",
			"REG_SZ",
			"/d",
			value,
			"/f",
		); err != nil {
			return err
		}
	} else if _, err := p.run(ctx, "reg", "delete", key, "/v", "ProxyServer", "/f"); err != nil {
		return err
	}
	if value := p.snapshot.extra["ProxyOverride"]; value != "" {
		if _, err := p.run(
			ctx,
			"reg",
			"add",
			key,
			"/v",
			"ProxyOverride",
			"/t",
			"REG_SZ",
			"/d",
			value,
			"/f",
		); err != nil {
			return err
		}
	} else if _, err := p.run(ctx, "reg", "delete", key, "/v", "ProxyOverride", "/f"); err != nil {
		return err
	}
	_, err := p.run(ctx, "rundll32", "user32.dll,UpdatePerUserSystemParameters")
	return err
}
