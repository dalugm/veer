package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Version reads the first line of Xray's version output with bounded time/output.
func Version(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	hideQueryWindow(cmd)
	cmd.WaitDelay = 100 * time.Millisecond
	var out versionOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("xray version check failed: %w", errors.Join(ctx.Err(), err))
	}
	if out.overflow {
		return "", errors.New("xray version output exceeds 4 KiB")
	}
	v := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	if v == "" {
		return "", errors.New("engine returned no version information")
	}
	return v, nil
}

type versionOutput struct {
	strings.Builder
	overflow bool
}

func (w *versionOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, 4096-w.Len())
	_, _ = w.Builder.Write(p[:keep])
	w.overflow = w.overflow || keep < n
	return n, nil
}
