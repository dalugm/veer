//go:build !windows

package coreupdate

import (
	"context"
	"fmt"
	"io"
	"os"
)

func replaceExecutable(ctx context.Context, staged, target, backup string) error {
	// Copy before publishing, including on filesystems without hard links.
	if err := copyPreviousExecutable(ctx, target, backup); err != nil {
		return fmt.Errorf("back up Xray executable: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(backup)
		return err
	}
	// Both files are on the same filesystem. Replacing the directory entry is
	// atomic on Unix and leaves the running process mapped to its old inode.
	if err := os.Rename(staged, target); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("replace Xray executable: %w", err)
	}
	return nil
}

func copyPreviousExecutable(ctx context.Context, target, backup string) (err error) {
	source, err := os.Open(target)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
		if err != nil {
			_ = os.Remove(backup)
		}
	}()
	n, err := io.Copy(out, contextReader{ctx, source})
	if err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("previous executable changed while backing up")
	}
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	after, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !os.SameFile(info, after) || info.Size() != after.Size() ||
		info.ModTime() != after.ModTime() ||
		info.Mode() != after.Mode() {
		return fmt.Errorf("xray executable changed while backing up")
	}
	return out.Close()
}
