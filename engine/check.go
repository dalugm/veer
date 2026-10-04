package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// ResolveXrayBinary finds an explicitly selected core, or falls back to a core
// beside the configuration before searching PATH.
func ResolveXrayBinary(requested, configDir string) (string, error) {
	if requested != "" {
		binary, err := exec.LookPath(requested)
		if err != nil {
			return "", fmt.Errorf("find Xray binary: %w", err)
		}
		return filepath.Abs(binary)
	}
	name := "xray"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	beside := filepath.Join(configDir, name)
	if binary, err := exec.LookPath(beside); err == nil {
		return filepath.Abs(binary)
	}
	binary, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New(
			"Xray not found beside the configuration or in PATH; select its executable",
		)
	}
	return filepath.Abs(binary)
}

// CheckConfig asks Xray to validate a configuration without starting listeners.
func CheckConfig(ctx context.Context, binary, configPath, assetDir string) error {
	return checkConfig(ctx, binary, configPath, assetDir, exec.CommandContext)
}

func checkConfig(
	ctx context.Context,
	binary, configPath, assetDir string,
	command func(context.Context, string, ...string) *exec.Cmd,
) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := command(ctx, binary, "run", "-test", "-c", configPath)
	cmd.Dir = assetDir
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "XRAY_LOCATION_ASSET="+assetDir)
	cmd.WaitDelay = 100 * time.Millisecond
	hideQueryWindow(cmd)
	// Core errors can contain client credentials. Callers receive the exit status;
	// interactive sessions collect sanitized diagnostic logs through session.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Xray configuration validation failed: %w", errors.Join(ctx.Err(), err))
	}
	return nil
}
