package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLogRotationRetainsThreeReadableArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	grants := 0
	grant := func(_ context.Context, f *os.File) error {
		grants++
		if f.Name() != path {
			t.Fatal("grant received an unexpected file")
		}
		return nil
	}
	for i := 1; i <= 4; i++ {
		writeLog(t, path, "connection "+strconv.Itoa(i)+"\n", os.O_TRUNC)
		if err := rotateLogFile(t.Context(), path, 10, grant); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("active file was not archived")
		}
	}
	if grants != 4 {
		t.Fatalf("read grants: %d", grants)
	}
	for i := 1; i <= 3; i++ {
		archive := path + "." + strconv.Itoa(i)
		data, err := os.ReadFile(archive)
		if err != nil || string(data) != "connection "+strconv.Itoa(5-i)+"\n" {
			t.Fatalf("archive %d: %q %v", i, data, err)
		}
		info, err := os.Stat(archive)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("archive permissions: %v %v", info, err)
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatal("excess archive retained")
	}
	writeLog(t, path, "small\n", os.O_TRUNC)
	if err := rotateLogFile(t.Context(), path, 10, grant); err != nil {
		t.Fatal(err)
	}
	if grants != 4 {
		t.Fatal("small log was rotated")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "small\n" {
		t.Fatalf("active log: %q %v", data, err)
	}
}

func TestLogRotationFailurePreservesCurrentFile(t *testing.T) {
	for _, mode := range []string{"cancel", "grant", "archive-symlink", "source-symlink"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "access.log")
			writeLog(t, path, "original\n", os.O_TRUNC)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			grant := func(context.Context, *os.File) error { return nil }
			switch mode {
			case "cancel":
				cancel()
			case "grant":
				grant = func(context.Context, *os.File) error { return errors.New("access denied") }
			case "archive-symlink":
				if err := os.Symlink(path, path+".1"); err != nil {
					t.Skip(err)
				}
			case "source-symlink":
				target := path
				path = filepath.Join(dir, "linked.log")
				if err := os.Symlink(target, path); err != nil {
					t.Skip(err)
				}
			}
			if err := rotateLogFile(ctx, path, 1, grant); err == nil {
				t.Fatal("unsafe rotation accepted")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "original\n" {
				t.Fatalf("current log lost: %q %v", data, err)
			}
		})
	}
}
