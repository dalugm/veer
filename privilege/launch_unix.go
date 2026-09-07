//go:build !windows

package privilege

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// Elevated reports whether the current process has administrator privileges.
func Elevated() bool { return os.Geteuid() == 0 }

// AuthenticateCommand returns the command used to obtain cached sudo authorization.
func AuthenticateCommand() *exec.Cmd { return exec.Command("sudo", "-v") }

func launchHelper(exe, address, token string) (<-chan error, error) {
	cmd := exec.Command("sudo", "-n", exe, "--veer-helper", address, token)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, stderr.String())
		}
		done <- err
	}()
	return done, nil
}
