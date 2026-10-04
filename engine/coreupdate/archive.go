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
	"strings"
)

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
