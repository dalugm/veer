package coreupdate

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

func checksumFor(data []byte) ([]byte, error) {
	var found []byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || (fields[0] != "SHA256=" && fields[0] != "SHA2-256=") {
			continue
		}
		if found != nil {
			return nil, errors.New("duplicate binary checksum in release")
		}
		if len(fields) != 2 {
			return nil, errors.New("invalid SHA-256 digest record")
		}
		hash, err := hex.DecodeString(fields[1])
		if err != nil || len(hash) != sha256.Size {
			return nil, errors.New("invalid binary checksum in release")
		}
		found = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	if found == nil {
		return nil, errors.New("release is missing the binary SHA-256 checksum")
	}
	return found, nil
}

func verifyDigest(data []byte, digest string) error {
	if digest == "" {
		return nil
	}
	hash := sha256.Sum256(data)
	if digest != "sha256:"+hex.EncodeToString(hash[:]) {
		return errors.New("release asset SHA-256 digest mismatch")
	}
	return nil
}

func (c *Client) downloadArchive(
	ctx context.Context,
	r Release,
	path string,
	want []byte,
) error {
	resp, err := c.get(ctx, assetURL(r.tag, r.binary.Name))
	if err != nil {
		return fmt.Errorf("download Xray: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	n, err := io.Copy(
		io.MultiWriter(f, hash),
		io.LimitReader(contextReader{ctx, resp.Body}, r.binary.Size+1),
	)
	if err != nil {
		return fmt.Errorf("download Xray: %w", err)
	}
	if n != r.binary.Size {
		return errors.New("downloaded Xray size does not match the release")
	}
	if !bytes.Equal(hash.Sum(nil), want) {
		return errors.New("xray SHA-256 checksum mismatch; existing executable retained")
	}
	if r.binary.Digest != "" && r.binary.Digest != "sha256:"+hex.EncodeToString(hash.Sum(nil)) {
		return errors.New("xray release digest mismatch")
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (c *Client) extractBinary(
	ctx context.Context,
	archive, staged string,
	mode os.FileMode,
) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open Xray ZIP: %w", err)
	}
	defer func() { _ = zr.Close() }()
	name := "xray"
	if c.goos == "windows" {
		name += ".exe"
	}
	var binary *zip.File
	for _, entry := range zr.File {
		if entry.Name != name {
			continue
		}
		if binary != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 ||
			entry.UncompressedSize64 > maxBinarySize {
			return errors.New("invalid or duplicate executable in Xray ZIP")
		}
		binary = entry
	}
	if binary == nil {
		return fmt.Errorf("xray ZIP does not contain %s", name)
	}
	in, err := binary.Open()
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, in}, maxBinarySize+1))
	if err != nil {
		return fmt.Errorf("extract Xray: %w", err)
	}
	if uint64(n) != binary.UncompressedSize64 {
		return errors.New("invalid extracted Xray size")
	}
	if err := out.Chmod(mode); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
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
