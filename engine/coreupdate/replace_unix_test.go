//go:build !windows

package coreupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type cancelDuringBackup struct {
	context.Context
	reads int
}

func (c *cancelDuringBackup) Err() error {
	c.reads++
	if c.reads > 1 {
		return context.Canceled
	}
	return nil
}

func TestCancelDuringBackupPreservesCurrentExecutable(t *testing.T) {
	dir := t.TempDir()
	target, staged, backup := filepath.Join(
		dir,
		"xray",
	), filepath.Join(
		dir,
		"next",
	), filepath.Join(
		dir,
		"previous",
	)
	if err := os.WriteFile(target, []byte("old executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelDuringBackup{Context: t.Context()}
	if err := replaceExecutable(ctx, staged, target, backup); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	assertContents(t, target, "old executable")
	assertContents(t, staged, "new executable")
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete backup retained: %v", err)
	}
}
