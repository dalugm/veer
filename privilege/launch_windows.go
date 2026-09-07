//go:build windows

package privilege

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Elevated reports whether the current process has an elevated Windows token.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// AuthenticateCommand returns nil because Windows authorization occurs through UAC.
func AuthenticateCommand() *exec.Cmd { return nil }

func launchHelper(exe, address, token string) (<-chan error, error) {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return nil, err
	}
	file, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	args := syscall.EscapeArg(
		"--veer-helper",
	) + " " + syscall.EscapeArg(
		address,
	) + " " + syscall.EscapeArg(
		token,
	)
	params, err := syscall.UTF16PtrFromString(args)
	if err != nil {
		return nil, err
	}
	result, _, callErr := syscall.NewLazyDLL("shell32.dll").
		NewProc("ShellExecuteW").
		Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), 0, 0)
	if result <= 32 {
		return nil, fmt.Errorf("UAC elevation failed (%d): %w", result, callErr)
	}
	return nil, nil
}
