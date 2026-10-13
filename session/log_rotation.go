package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/dalugm/veer/engine"
)

const (
	logRotateSize = 50 << 20
	logBackups    = 3
)

// Rotate only between managed processes: Xray keeps its log descriptors open
// while running, so renaming or truncating them live would lose continuity.
func (c *Controller) rotateLogFiles(ctx context.Context, files []engine.LogFile) {
	for _, file := range files {
		if ctx.Err() != nil {
			return
		}
		if err := rotateLogFile(ctx, file.Path, logRotateSize, c.grantLogRead); err != nil {
			c.Log(fmt.Sprintf("[Warning] veer: log rotation: %v", err))
		}
	}
}

func rotateLogFile(
	ctx context.Context,
	path string,
	limit int64,
	grant func(context.Context, *os.File) error,
) error {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return errors.New("log rotation requires a regular file")
	}
	if before.Size() < limit {
		return nil
	}
	file, err := openLogFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	current, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, current) {
		return errors.New("log destination changed before rotation")
	}
	// For elevated sessions this also verifies that the file may be managed
	// for the originating account; archived inodes retain their read ACL.
	if grant != nil {
		if err := grant(ctx, file); err != nil {
			return fmt.Errorf("log archive read access: %w", err)
		}
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	present := make([]bool, logBackups+1)
	for i := 1; i <= logBackups; i++ {
		info, err := root.Lstat(name + "." + strconv.Itoa(i))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("log archive destination is not a regular file")
		}
		present[i] = true
	}
	latest, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !os.SameFile(current, latest) {
		return errors.New("log destination changed before rotation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := logBackups - 1; i >= 1; i-- {
		if present[i] {
			if err := root.Rename(
				name+"."+strconv.Itoa(i),
				name+"."+strconv.Itoa(i+1),
			); err != nil {
				return err
			}
		}
	}
	return root.Rename(name, name+".1")
}
