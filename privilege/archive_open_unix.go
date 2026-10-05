//go:build !windows

package privilege

import "os"

func openArchiveFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
}
