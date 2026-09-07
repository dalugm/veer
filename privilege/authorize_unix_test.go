//go:build !windows

package privilege

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSudoPasswordUsesStdin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-sudo")
	script := `#!/bin/sh
if [ "$1" = "-n" ] && [ "$2" = "-v" ] && [ "$#" = 2 ]; then exit 0; fi
[ "$#" = 4 ] && [ "$1" = "-S" ] && [ "$2" = "-p" ] && [ "$3" = "" ] && [ "$4" = "-v" ] || exit 3
IFS= read -r password
[ "$password" = " test password " ] || exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sudoAuthorization(t.Context(), path, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := sudoAuthorization(t.Context(), path, []byte(" test password "), false); err != nil {
		t.Fatal(err)
	}
	if err := sudoAuthorization(t.Context(), path, []byte("wrong"), false); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err := sudoAuthorization(t.Context(), path, []byte("line\nbreak"), false); err == nil {
		t.Fatal("multiple password lines accepted")
	}
}

func TestSudoAuthorizationCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-sudo")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := sudoAuthorization(
		ctx,
		path,
		[]byte("test"),
		false,
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("cancellation: %v", err)
	}
}
