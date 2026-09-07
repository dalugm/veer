package engine

import (
	"os/exec"
	"syscall"
)

// Elevated helpers have no console; avoid opening one on each stats poll.
func hideQueryWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
