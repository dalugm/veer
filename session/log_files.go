package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dalugm/veer/engine"
)

const logReadLimit = 64 << 10

// A follower owns its descriptor and offset. Existing files start at EOF; new
// and rotated files start at zero. Reads and pending lines stay bounded.
type logFollower struct {
	config    engine.LogFile
	file      *os.File
	offset    int64
	pending   string
	lastError string
	grant     func(context.Context, *os.File) error
	log       func(string)
}

func (c *Controller) prepareLogFiles(ctx context.Context, configs []engine.LogFile) []*logFollower {
	var followers []*logFollower
	for _, config := range configs {
		f := &logFollower{config: config, grant: c.grantLogRead, log: c.Log}
		if err := f.open(ctx, true); err != nil {
			f.report(err)
		}
		followers = append(followers, f)
	}
	return followers
}

func (f *logFollower) open(ctx context.Context, existing bool) error {
	before, err := os.Lstat(f.config.Path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return errors.New("log destination is not a regular file")
	}
	file, err := openLogFile(f.config.Path)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		_ = file.Close()
		if err != nil {
			return err
		}
		return errors.New("log destination changed while opening")
	}
	f.file, f.offset = file, 0
	if existing {
		f.offset = info.Size()
	}
	if f.grant != nil && ctx.Err() == nil {
		if err := f.grant(ctx, file); err != nil {
			f.log(fmt.Sprintf("[Warning] veer: %s log read access: %v", f.config.Source, err))
		}
	}
	return nil
}

func (f *logFollower) report(err error) {
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	message := fmt.Sprint(err)
	if message != f.lastError {
		f.log(fmt.Sprintf("[Warning] veer: %s log monitoring: %s", f.config.Source, message))
		f.lastError = message
	}
}

func (f *logFollower) close() {
	if f.file != nil {
		_ = f.file.Close()
		f.file = nil
	}
}

func (f *logFollower) append(data string) {
	f.pending += data
	for {
		line, rest, ok := strings.Cut(f.pending, "\n")
		if !ok {
			break
		}
		f.log("[" + f.config.Source + "] " + strings.TrimSuffix(line, "\r"))
		f.pending = rest
	}
	if len(f.pending) > 8192 {
		f.log("[" + f.config.Source + "] " + f.pending[:4096] + "…")
		f.pending = ""
	}
}

func (f *logFollower) read() error {
	info, err := f.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < f.offset {
		f.offset, f.pending = 0, ""
	}
	var buffer [logReadLimit]byte
	n, err := f.file.ReadAt(buffer[:], f.offset)
	f.offset += int64(n)
	f.append(string(buffer[:n]))
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (f *logFollower) poll(ctx context.Context) {
	if f.file != nil {
		if err := f.read(); err != nil {
			f.report(err)
		}
		info, err := os.Lstat(f.config.Path)
		current, statErr := f.file.Stat()
		if err == nil && statErr == nil && os.SameFile(info, current) {
			return
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			f.report(err)
			return
		}
		if statErr == nil && f.offset < current.Size() {
			return // Drain the rotated descriptor before switching to its replacement.
		}
		if f.pending != "" {
			f.append("\n")
		}
		f.close()
		f.pending = ""
	}
	if err := f.open(ctx, false); err != nil {
		f.report(err)
		return
	}
	f.lastError = ""
	if err := f.read(); err != nil {
		f.report(err)
	}
}

func followLogFiles(ctx context.Context, followers []*logFollower) {
	defer closeLogFiles(followers)
	if len(followers) == 0 {
		return
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, file := range followers {
			file.poll(ctx)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			for _, file := range followers {
				for {
					previous, offset := file.file, file.offset
					file.poll(ctx)
					if file.file == nil {
						break
					}
					info, err := file.file.Stat()
					if err != nil || file.offset >= info.Size() {
						break
					}
					if previous == file.file && offset == file.offset {
						break // A failed read must not prevent process cleanup.
					}
				}
				if file.pending != "" {
					file.append("\n")
				}
			}
			return
		}
	}
}

func closeLogFiles(files []*logFollower) {
	for _, file := range files {
		file.close()
	}
}
