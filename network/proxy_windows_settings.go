package network

import (
	"context"
	"errors"
	"net"
	"strings"
)

const windowsProxyKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type registryValue struct{ kind, data string }

func parseWindowsProxy(s *proxySnapshot, output string) {
	s.registry = make(map[string]registryValue)
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != "ProxyEnable" && fields[0] != "ProxyServer") {
			continue
		}
		_, data, _ := strings.Cut(line, fields[1])
		s.registry[fields[0]] = registryValue{kind: fields[1], data: strings.TrimSpace(data)}
	}
}

func (p *proxyChange) applyWindows(ctx context.Context, host, port string) error {
	for _, value := range []struct{ name, kind, data string }{
		{"ProxyServer", "REG_SZ", "socks=" + net.JoinHostPort(host, port)},
		{"ProxyEnable", "REG_DWORD", "1"},
	} {
		p.touched[value.name] = true
		if _, err := p.run(
			ctx,
			"reg",
			"add",
			windowsProxyKey,
			"/v",
			value.name,
			"/t",
			value.kind,
			"/d",
			value.data,
			"/f",
		); err != nil {
			return err
		}
	}
	return p.notify(ctx)
}

func (p *proxyChange) restoreWindows(ctx context.Context) error {
	// Query before deleting: an absent original value may already have been
	// removed by an earlier restoration attempt or never successfully created.
	out, err := p.run(ctx, "reg", "query", windowsProxyKey)
	if err != nil {
		return err
	}
	var current proxySnapshot
	parseWindowsProxy(&current, out)
	var result error
	for _, name := range []string{"ProxyServer", "ProxyEnable"} {
		if !p.touched[name] {
			continue
		}
		if value, exists := p.snapshot.registry[name]; exists {
			_, err = p.run(
				ctx,
				"reg",
				"add",
				windowsProxyKey,
				"/v",
				name,
				"/t",
				value.kind,
				"/d",
				value.data,
				"/f",
			)
		} else if _, exists := current.registry[name]; exists {
			_, err = p.run(ctx, "reg", "delete", windowsProxyKey, "/v", name, "/f")
		} else {
			err = nil
		}
		result = errors.Join(result, err)
	}
	return errors.Join(result, p.notify(ctx))
}
