package coreupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/mod/semver"
)

// Result describes an installed update and its retained previous executable.
type Result struct {
	Version       string
	VersionOutput string
	BackupPath    string
	TargetPath    string
}

// Install downloads, verifies and replaces the executable. The caller must
// obtain explicit user confirmation and serialize installations. Cancellation
// is honored until the final rename transaction; rollback must finish once begun.
func (c *Client) Install(
	ctx context.Context,
	binary, current string,
	release Release,
) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	current = ParseVersion(current)
	if current == "" {
		return Result{}, ErrUnknownVersion
	}
	name, err := c.archiveName()
	if err != nil {
		return Result{}, err
	}
	v := normalizeVersion(release.tag)
	if v == "" || v != release.Version || semver.Compare(v, current) <= 0 ||
		release.binary.Name != name || release.binary.Size <= 0 || release.binary.Size > maxBinarySize ||
		release.checksums.Name != name+".dgst" {
		return Result{}, errors.New("invalid or outdated update; check for updates again")
	}
	target, err := exec.LookPath(binary)
	if err != nil {
		return Result{}, fmt.Errorf("locate Xray executable: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return Result{}, err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Xray executable: %w", err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		return Result{}, err
	}
	if !before.Mode().IsRegular() || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return Result{}, errors.New("core update requires a regular, non-setuid executable")
	}
	installed, err := c.version(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if ParseVersion(installed) != current {
		return Result{}, errors.New("xray version changed; check for updates again")
	}
	stageDir, err := os.MkdirTemp(filepath.Dir(target), ".veer-update-*")
	if err != nil {
		return Result{}, fmt.Errorf(
			"cannot write beside Xray; update from a writable installation: %w",
			err,
		)
	}
	staged := filepath.Join(stageDir, "next")
	if c.goos == "windows" {
		staged += ".exe"
	}
	archive := filepath.Join(stageDir, "release.zip")
	backup := filepath.Join(stageDir, "previous.exe")
	// Never recursively delete the directory: it may contain the only recoverable
	// old executable after a failed rollback, or a still-running Windows image.
	defer func() { _ = os.Remove(staged); _ = os.Remove(archive); _ = os.Remove(stageDir) }()
	checksums, err := c.read(ctx, assetURL(release.tag, release.checksums.Name), maxChecksumsSize)
	if err != nil {
		return Result{}, fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyDigest(checksums, release.checksums.Digest); err != nil {
		return Result{}, err
	}
	want, err := checksumFor(checksums)
	if err != nil {
		return Result{}, err
	}
	if err := c.downloadArchive(ctx, release, archive, want); err != nil {
		return Result{}, err
	}
	if err := c.extractBinary(ctx, archive, staged, before.Mode().Perm()); err != nil {
		return Result{}, err
	}
	nextVersion, err := c.version(ctx, staged)
	if err != nil {
		return Result{}, fmt.Errorf("verify downloaded Xray: %w", err)
	}
	if !matchesRelease(nextVersion, release.Version) {
		return Result{}, errors.New("downloaded Xray version does not match the release")
	}
	after, err := os.Lstat(target)
	if err != nil {
		return Result{}, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() ||
		before.ModTime() != after.ModTime() ||
		before.Mode() != after.Mode() {
		return Result{}, errors.New(
			"xray executable changed during download; check again",
		)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := replaceExecutable(ctx, staged, target, backup); err != nil {
		return Result{}, err
	}
	return Result{
		Version:       release.Version,
		VersionOutput: nextVersion,
		BackupPath:    backup,
		TargetPath:    target,
	}, nil
}

func replaceWithBackup(staged, target, backup string, rename func(string, string) error) error {
	if err := rename(target, backup); err != nil {
		return fmt.Errorf("back up Xray executable: %w", err)
	}
	if err := rename(staged, target); err != nil {
		if rollbackErr := rename(backup, target); rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("replace Xray: %w", err),
				fmt.Errorf(
					"restore failed; recover the previous executable from %s: %w",
					backup,
					rollbackErr,
				),
			)
		}
		return fmt.Errorf("replace Xray (previous executable restored): %w", err)
	}
	return nil
}
