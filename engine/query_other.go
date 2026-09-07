//go:build !windows

package engine

import "os/exec"

func hideQueryWindow(*exec.Cmd) {}
