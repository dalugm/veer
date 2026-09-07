package privilege

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// AuthorizationCached checks sudo without prompting or reading terminal input.
func AuthorizationCached(ctx context.Context) (bool, error) {
	err := sudoAuthorization(ctx, "sudo", nil, true)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return err == nil, err
}

// Authorize passes one password to sudo over stdin. It never includes sudo
// output in returned errors because PAM modules may echo sensitive input.
// The caller owns clearing password after this call returns.
func Authorize(ctx context.Context, password []byte) error {
	if err := sudoAuthorization(ctx, "sudo", password, false); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("authorization failed; retry or use Ctrl+T for system authentication")
	}
	return nil
}

func sudoAuthorization(ctx context.Context, binary string, password []byte, check bool) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := []string{"-S", "-p", "", "-v"}
	if check {
		args = []string{"-n", "-v"}
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = 200 * time.Millisecond
	if !check {
		if bytes.ContainsAny(password, "\r\n") {
			return errors.New("password must be a single line")
		}
		input := make([]byte, len(password)+1)
		copy(input, password)
		input[len(password)] = '\n'
		defer clear(input)
		cmd.Stdin = bytes.NewReader(input)
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
