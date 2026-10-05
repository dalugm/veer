//go:build !windows

package session

import "os"

func openLogFile(path string) (*os.File, error) { return os.Open(path) }
