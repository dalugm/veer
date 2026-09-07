package session

import (
	"net"
	"testing"
)

func TestTUNReadinessRequiresExpectedLiveInterface(t *testing.T) {
	old := net.Interface{Name: "utun3", Index: 3, Flags: net.FlagUp | net.FlagPointToPoint}
	fresh := net.Interface{Name: "utun4", Index: 4, Flags: net.FlagUp | net.FlagPointToPoint}
	linux := net.Interface{Name: "veer0", Index: 5, Flags: net.FlagUp | net.FlagPointToPoint}
	for _, tc := range []struct {
		name      string
		current   []net.Interface
		requested string
		want      bool
	}{
		{"existing VPN", []net.Interface{old}, "", false},
		{"named existing VPN", []net.Interface{old}, "utun3", false},
		{"new mac TUN", []net.Interface{old, fresh}, "", true},
		{"named Linux TUN", []net.Interface{old, linux}, "veer0", true},
		{"wrong named TUN", []net.Interface{old, fresh}, "veer0", false},
		{"down TUN", []net.Interface{{Name: "veer0", Index: 5}}, "veer0", false},
		{"ethernet", []net.Interface{{Name: "en0", Index: 8, Flags: net.FlagUp}}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tunReady([]net.Interface{old}, tc.current, tc.requested); got != tc.want {
				t.Fatalf("ready=%v", got)
			}
		})
	}
}
