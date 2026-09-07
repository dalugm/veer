//go:build !windows

package session

import (
	"os"
	"os/exec"
)

func configureProcess(*exec.Cmd)  {}
func interrupt(c *exec.Cmd) error { return c.Process.Signal(os.Interrupt) }
