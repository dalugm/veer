//go:build !windows

package network

import "context"

func notifyProxyChanged(context.Context) error { return nil }
