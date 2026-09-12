package coreupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Restore publishes a local backup and retains the replaced executable.
// It requires explicit confirmation and makes no network requests.
func (c *Client) Restore(
	ctx context.Context,
	binary, backup, expectedTarget string,
) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	target, err := exec.LookPath(binary)
	if err != nil {
		return Result{}, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return Result{}, err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return Result{}, err
	}
	if target != expectedTarget {
		return Result{}, errors.New(
			"configured Xray now resolves to a different installation; backup retained",
		)
	}
	before, err := os.Lstat(target)
	if err != nil {
		return Result{}, err
	}
	if !before.Mode().IsRegular() || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return Result{}, errors.New("core restore requires a regular, non-setuid executable")
	}
	source, err := os.Open(backup)
	if err != nil {
		return Result{}, fmt.Errorf("open previous core: %w", err)
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return Result{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBinarySize ||
		os.SameFile(before, info) {
		return Result{}, errors.New("invalid previous core backup")
	}
	dir, err := os.MkdirTemp(filepath.Dir(target), ".veer-update-*")
	if err != nil {
		return Result{}, err
	}
	staged := filepath.Join(dir, "next")
	if c.goos == "windows" {
		staged += ".exe"
	}
	previous := filepath.Join(dir, "previous.exe")
	defer func() { _ = os.Remove(staged); _ = os.Remove(dir) }()
	out, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = out.Close() }()
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, source}, maxBinarySize+1))
	if err != nil {
		return Result{}, err
	}
	if n != info.Size() {
		return Result{}, errors.New("previous core changed while restoring")
	}
	if err := out.Chmod(before.Mode().Perm()); err != nil {
		return Result{}, err
	}
	if err := out.Sync(); err != nil {
		return Result{}, err
	}
	if err := out.Close(); err != nil {
		return Result{}, err
	}
	v, err := c.version(ctx, staged)
	if err != nil {
		return Result{}, fmt.Errorf("verify previous core: %w", err)
	}
	if ParseVersion(v) == "" {
		return Result{}, ErrUnknownVersion
	}
	after, err := os.Lstat(target)
	if err != nil {
		return Result{}, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() ||
		before.ModTime() != after.ModTime() ||
		before.Mode() != after.Mode() {
		return Result{}, errors.New("xray executable changed during restore")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := replaceExecutable(ctx, staged, target, previous); err != nil {
		return Result{}, err
	}
	return Result{
		Version:       ParseVersion(v),
		VersionOutput: v,
		BackupPath:    previous,
		TargetPath:    target,
	}, nil
}
