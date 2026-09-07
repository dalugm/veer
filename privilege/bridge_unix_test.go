//go:build !windows

package privilege

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

func TestBridgeCore(t *testing.T) {
	if os.Getenv("VEER_BRIDGE_CORE") != "1" {
		return
	}
	for _, arg := range os.Args {
		if arg == "-test" {
			os.Exit(0)
		}
	}
	var cfg struct {
		Inbounds []struct {
			Port int `json:"port"`
		} `json:"inbounds"`
	}
	data, err := os.ReadFile(os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(2)
	}
	if json.Unmarshal(data, &cfg) != nil {
		os.Exit(3)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Inbounds[0].Port))
	if err != nil {
		os.Exit(4)
	}
	defer func() { _ = listener.Close() }()
	fmt.Println("bridge core online")
	for {
		c, e := listener.Accept()
		if e != nil {
			os.Exit(0)
		}
		_ = c.Close()
	}
}

func TestHelperSessionStopsOnDisconnect(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelParent), func(t *testing.T) {
			t.Setenv("VEER_BRIDGE_CORE", "1")
			exe, _ := os.Executable()
			dir := t.TempDir()
			binary := filepath.Join(dir, "xray")
			script := "#!/bin/sh\nexec '" + strings.ReplaceAll(
				exe,
				"'",
				"'\\''",
			) + "' -test.run=TestBridgeCore -- \"$@\"\n"
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			config := filepath.Join(dir, "config.json")
			if err := os.WriteFile(
				config,
				[]byte(
					fmt.Sprintf(
						`{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":%d}],"outbounds":[{"protocol":"freedom"}]}`,
						port,
					),
				),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			served := make(chan error, 1)
			r := &remote{launch: func(_ string, addr, token string) (<-chan error, error) {
				go func() { served <- Serve(t.Context(), addr, token) }()
				return nil, nil
			}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if err := r.Start(ctx, engine.Options{Binary: binary, Config: config}); err != nil {
				t.Fatal(err)
			}
			pid := r.Snapshot().PID
			if pid == 0 {
				t.Fatal("missing helper-owned child")
			}
			if cancelParent {
				cancel()
			} else {
				stopCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
				defer stop()
				if err := r.Stop(stopCtx); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-served:
			case <-time.After(6 * time.Second):
				t.Fatal("helper did not finish cleanup")
			}
			if err := syscall.Kill(pid, 0); err == nil {
				t.Fatal("core survived helper disconnect")
			}
			<-r.done
			if state := r.Snapshot().State; state != session.Stopped {
				t.Fatalf("final state %s", state)
			}
		})
	}
}
