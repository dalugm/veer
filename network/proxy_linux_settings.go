package network

import (
	"context"
	"errors"
	"strings"
)

type linuxProxySetting struct{ schema, key string }

func (s linuxProxySetting) name() string { return s.schema + ":" + s.key }
func trimSetting(v string) string        { return strings.TrimSpace(v) }

// Protocol-specific hosts override SOCKS in GNOME. Clear them while using
// Veer's SOCKS listener, and restore every setting before restoring the mode.
var linuxProxySettings = []linuxProxySetting{
	{"org.gnome.system.proxy", "use-same-proxy"},
	{"org.gnome.system.proxy.http", "host"},
	{"org.gnome.system.proxy.https", "host"},
	{"org.gnome.system.proxy.ftp", "host"},
	{"org.gnome.system.proxy.socks", "host"},
	{"org.gnome.system.proxy.socks", "port"},
	{"org.gnome.system.proxy", "mode"},
}

func (p *proxyChange) applyLinux(ctx context.Context, host, port string) error {
	values := []string{"false", "''", "''", "''", "'" + host + "'", port, "'manual'"}
	for i, setting := range linuxProxySettings {
		if _, err := p.run(
			ctx,
			"gsettings",
			"set",
			setting.schema,
			setting.key,
			values[i],
		); err != nil {
			return err
		}
	}
	return nil
}

func (p *proxyChange) restoreLinux(ctx context.Context) error {
	var result error
	for _, setting := range linuxProxySettings {
		_, err := p.run(
			ctx,
			"gsettings",
			"set",
			setting.schema,
			setting.key,
			p.snapshot.extra[setting.name()],
		)
		result = errors.Join(result, err)
	}
	return result
}
