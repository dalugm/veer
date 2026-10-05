//go:build !windows

package privilege

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestLogReaderMatchesOriginatingAccount(t *testing.T) {
	t.Setenv("SUDO_UID", "501")
	for _, reader := range []string{"", "not-a-uid", "502"} {
		if validateLogReader(reader) == nil {
			t.Fatalf("accepted %q", reader)
		}
	}
	if err := validateLogReader("501"); err != nil {
		t.Fatal(err)
	}
}

func TestLogACLTargetsVerifiedDescriptor(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	reader := strconv.Itoa(os.Getuid())
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			called := false
			err := setUnixLogRead(
				t.Context(),
				f,
				reader,
				platform,
				func(ctx context.Context, file *os.File, program string, args ...string) error {
					called = true
					if file != f {
						t.Fatal("wrong descriptor")
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("ACL command has no deadline")
					}
					if platform == "linux" {
						if program != "/usr/bin/setfacl" ||
							!reflect.DeepEqual(
								args,
								[]string{"-m", "u:" + reader + ":r", "/proc/self/fd/3"},
							) {
							t.Fatalf("%s %v", program, args)
						}
					} else if program != "/bin/chmod" || len(args) != 3 || args[0] != "+a" || args[2] != "/dev/fd/3" {
						t.Fatalf("%s %v", program, args)
					}
					return nil
				},
			)
			if err != nil || !called {
				t.Fatalf("called=%v err=%v", called, err)
			}
		})
	}
	failure := errors.New("unavailable")
	if err := setUnixLogRead(
		t.Context(),
		f,
		reader,
		"linux",
		func(context.Context, *os.File, string, ...string) error { return failure },
	); !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("lost ACL error: %v", err)
	}
}
