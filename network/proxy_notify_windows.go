package network

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

var internetSetOption = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")

func notifyProxyChanged(ctx context.Context) error {
	// WinINet reads the changed per-user registry settings after these options.
	for _, option := range []uintptr{39, 37} { // SETTINGS_CHANGED, REFRESH
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, _, err := internetSetOption.Call(0, option, 0, 0)
		if ok == 0 {
			return fmt.Errorf("notify Windows proxy change (option %d): %w", option, err)
		}
	}
	return nil
}
