package privilege

import (
	"errors"
	"os"
)

// The unprivileged process creates this private file. The helper only appends
// to the verified, user-owned inode; it never creates an arbitrary destination.
func openSessionArchive(path, reader string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("session archive must be a regular file")
	}
	f, err := openArchiveFile(path)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err == nil && (!os.SameFile(before, after) || !after.Mode().IsRegular()) {
		err = errors.New("session archive changed while opening")
	}
	if err == nil {
		err = validateArchiveOwner(f, reader)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
