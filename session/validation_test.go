package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dalugm/veer/engine"
)

func TestFeatureCheckPrecedesNativeValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, tt := range []struct {
		version string
		bad     bool
	}{
		{"26.9.30", true}, {"26.10.10", false},
	} {
		t.Run(tt.version, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "xray")
			marker := filepath.Join(dir, "validated")
			t.Setenv("VEER_VALIDATION_MARKER", marker)
			script := "#!/bin/sh\nif [ \"$1\" = version ]; then\n echo 'Xray " + tt.version + "'\n exit 0\nfi\n: > \"$VEER_VALIDATION_MARKER\"\n"
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			c := New(fakeEngine{})
			plan := engine.Plan{
				Binary:    binary,
				CheckArgs: []string{"run", "-test"},
				Dir:       dir,
				Info:      engine.Info{LuaDNS: true},
			}
			if err := c.validatePlan(t.Context(), plan); (err != nil) != tt.bad {
				t.Fatalf("validation: %v", err)
			}
			_, err := os.Stat(marker)
			if tt.bad && !os.IsNotExist(err) {
				t.Fatal("unsupported Lua reached native validation")
			}
			if !tt.bad && err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := c.validatePlan(ctx, plan); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}
