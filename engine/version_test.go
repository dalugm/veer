package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVersionReadsOnlyBoundedFirstLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, tt := range []struct {
		script, want string
		bad          bool
	}{
		{"printf 'Xray 26.3.27 (Xray, Penetrates Everything.)\\nextra line\\n'", "Xray 26.3.27 (Xray, Penetrates Everything.)", false},
		{"exit 1", "", true},
		{"printf ''", "", true},
		{"head -c 5000 /dev/zero", "", true},
	} {
		path := filepath.Join(t.TempDir(), "xray")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tt.script+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := Version(t.Context(), path)
		if (err != nil) != tt.bad || got != tt.want {
			t.Fatalf("version: %q %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Version(ctx, "missing"); err == nil {
		t.Fatal("cancelled version query succeeded")
	}
}
