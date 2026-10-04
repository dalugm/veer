package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckConfigUsesCoreValidationAndAssetDirectory(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "client.json")
	if err := os.WriteFile(config, []byte(`{"outbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestConfigCheckHelper", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
		cmd.Env = append(os.Environ(), "VEER_CONFIG_CHECK_HELPER=1", "VEER_EXPECTED_ASSETS="+dir)
		return cmd
	}
	if err := checkConfig(t.Context(), "fake-xray", config, dir, command); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkConfig(
		ctx,
		"fake-xray",
		config,
		dir,
		command,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestConfigCheckHelper(t *testing.T) {
	if os.Getenv("VEER_CONFIG_CHECK_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) != 4 || args[0] != "run" || args[1] != "-test" || args[2] != "-c" {
		os.Exit(2)
	}
	dir, err := os.Getwd()
	if err != nil {
		os.Exit(3)
	}
	actual, actualErr := os.Stat(dir)
	expected, expectedErr := os.Stat(os.Getenv("VEER_EXPECTED_ASSETS"))
	if actualErr != nil || expectedErr != nil || !os.SameFile(actual, expected) ||
		os.Getenv("XRAY_LOCATION_ASSET") != os.Getenv("VEER_EXPECTED_ASSETS") {
		os.Exit(3)
	}
	data, err := os.ReadFile(args[3])
	if err != nil || !json.Valid(data) {
		os.Exit(4)
	}
	os.Exit(0)
}
