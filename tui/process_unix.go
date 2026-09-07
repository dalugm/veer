//go:build !windows

package tui

import "os/exec"

func hideProcess(*exec.Cmd) {}
