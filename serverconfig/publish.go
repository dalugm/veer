package serverconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

type pendingFile struct {
	path, staged, backup string
	data, expected       []byte
	original             os.FileInfo
	backed, installed    bool
}

// Stage beside each destination so every rename stays on its filesystem.
// Publish the server last; a failed publication restores all earlier files.
// Callers must serialize writes to a given set of destinations.
func publishFiles(files []pendingFile, rename func(string, string) error) error {
	directories := map[string]string{}
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			for _, dir := range directories {
				_ = os.RemoveAll(dir)
			}
		}
	}()
	seen := map[string]bool{}
	for i := range files {
		f := &files[i]
		path, err := filepath.Abs(f.path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return err
		}
		f.path = filepath.Join(parent, filepath.Base(path))
		if seen[f.path] {
			return errors.New("server and client destinations must be distinct")
		}
		seen[f.path] = true
		f.original, err = os.Lstat(f.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if f.original != nil && !f.original.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file: %s", f.path)
		}
		if f.expected != nil {
			current, err := os.ReadFile(f.path)
			if err != nil || !bytes.Equal(current, f.expected) {
				return errors.New("server config changed; retry adding the user")
			}
		}
		dir := directories[parent]
		if dir == "" {
			dir, err = os.MkdirTemp(parent, ".veer-write-*")
			if err != nil {
				return err
			}
			directories[parent] = dir
		}
		f.staged = filepath.Join(dir, "next-"+strconv.Itoa(i))
		f.backup = filepath.Join(dir, "previous-"+strconv.Itoa(i))
		if err := stageFile(f.staged, f.data); err != nil {
			return err
		}
	}
	// Detect changes made while staging before touching any destination.
	for _, f := range files {
		current, err := os.Lstat(f.path)
		if f.original == nil && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || f.original == nil || !os.SameFile(f.original, current) ||
			f.original.Size() != current.Size() || f.original.ModTime() != current.ModTime() ||
			f.original.Mode() != current.Mode() {
			return fmt.Errorf("destination changed before publication: %s", f.path)
		}
	}
	rollback := func(cause error) error {
		var failures []error
		for _, f := range slices.Backward(files) {
			var err error
			if f.backed {
				err = rename(f.backup, f.path)
			} else if f.installed {
				err = os.Remove(f.path)
			}
			if err != nil {
				keepRecovery = true
				failures = append(
					failures,
					fmt.Errorf(
						"restore %s (recovery files in %s): %w",
						f.path,
						filepath.Dir(f.backup),
						err,
					),
				)
			}
		}
		return errors.Join(append([]error{cause}, failures...)...)
	}
	for i := range files {
		f := &files[i]
		if f.original != nil {
			if err := rename(f.path, f.backup); err != nil {
				return rollback(fmt.Errorf("back up %s: %w", f.path, err))
			}
			f.backed = true
		}
		if err := rename(f.staged, f.path); err != nil {
			return rollback(fmt.Errorf("publish %s: %w", f.path, err))
		}
		f.installed = true
	}
	return nil
}

func stageFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	return errors.Join(writeErr, syncErr, f.Close())
}
