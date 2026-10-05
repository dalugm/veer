//go:build !windows

package privilege

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

func currentLogReader() (string, error) { return strconv.Itoa(os.Getuid()), nil }

func validateLogReader(reader string) error {
	if _, err := strconv.ParseUint(reader, 10, 32); err != nil {
		return errors.New("invalid log reader UID")
	}
	if expected := os.Getenv("SUDO_UID"); expected != "" && reader != expected {
		return errors.New("log reader does not match sudo's originating account")
	}
	return nil
}

func grantLogRead(ctx context.Context, file *os.File, reader string) error {
	uid, err := strconv.ParseUint(reader, 10, 32)
	if err != nil {
		return errors.New("invalid log reader UID")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return errors.New("log is not a regular file")
	}
	if uint64(stat.Uid) == uid || uid == 0 {
		return nil
	}
	// Do not grant access to arbitrary privileged files selected as log paths.
	// Automatically managed logs must live in the originating user's directory.
	parent, err := os.Stat(filepath.Dir(file.Name()))
	if err != nil {
		return err
	}
	owner, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || uint64(owner.Uid) != uid || stat.Uid != 0 {
		return errors.New(
			"automatic log access requires a log directory owned by the originating user",
		)
	}
	return setUnixLogRead(ctx, file, reader, runtime.GOOS, runLogACL)
}

func runLogACL(ctx context.Context, file *os.File, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	// Modify the already verified inode, even if the pathname is replaced during
	// rotation. Never follow a new symlink from an elevated chmod/setfacl command.
	cmd.ExtraFiles = []*os.File{file}
	return cmd.Run()
}

func setUnixLogRead(
	ctx context.Context,
	file *os.File,
	reader, platform string,
	run func(context.Context, *os.File, string, ...string) error,
) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	switch platform {
	case "darwin":
		account, err := user.LookupId(reader)
		if err != nil {
			return err
		}
		return run(
			ctx,
			file,
			"/bin/chmod",
			"+a",
			"user:"+account.Username+" allow read",
			"/dev/fd/3",
		)
	case "linux":
		if err := run(
			ctx,
			file,
			"/usr/bin/setfacl",
			"-m",
			"u:"+reader+":r",
			"/proc/self/fd/3",
		); err != nil {
			return fmt.Errorf("setfacl failed (Linux requires the acl package): %w", err)
		}
		return nil
	default:
		return errors.New("automatic log read access is unsupported on this platform")
	}
}

func validateArchiveOwner(file *os.File, reader string) error {
	uid, err := strconv.ParseUint(reader, 10, 32)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Uid) != uid || info.Mode().Perm()&0o077 != 0 {
		return errors.New("session archive must be private and owned by the originating user")
	}
	return nil
}
