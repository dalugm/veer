package network

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func parseMacProxy(s *proxySnapshot, output string) error {
	seenEnabled := false
	seenServer := false
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Enabled":
			seenEnabled = true
			s.enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "Server":
			seenServer = true
			s.extra["server"] = strings.TrimSpace(value)
		case "Port":
			s.extra["port"] = strings.TrimSpace(value)
		}
	}
	port, err := strconv.Atoi(s.extra["port"])
	if !seenEnabled || !seenServer || err != nil || port < 0 || port > 65535 {
		return errors.New("invalid macOS SOCKS proxy settings")
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
	_, restoreErr := p.run(
		ctx,
		"networksetup",
		"-setsocksfirewallproxy",
		p.snapshot.service,
		p.snapshot.extra["server"],
		p.snapshot.extra["port"],
	)
	state := "off"
	if p.snapshot.enabled {
		state = "on"
	}
	_, err := p.run(ctx, "networksetup", "-setsocksfirewallproxystate", p.snapshot.service, state)
	return errors.Join(restoreErr, err)
}
