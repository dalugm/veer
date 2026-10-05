package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dalugm/veer/engine"
)

func writeLog(t *testing.T, path, data string, flags int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|flags, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLogFollowerCreationTruncationAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	var logs []string
	grants := 0
	f := &logFollower{
		config: engine.LogFile{Path: path, Source: "access"},
		log:    func(s string) { logs = append(logs, s) },
		grant:  func(context.Context, *os.File) error { grants++; return nil },
	}
	defer f.close()
	f.poll(t.Context()) // Missing files are created by the core after launch.
	writeLog(t, path, "first\npartial", os.O_TRUNC)
	f.poll(t.Context())
	writeLog(t, path, " completed\n", os.O_APPEND)
	f.poll(t.Context())
	writeLog(t, path, "short\n", os.O_TRUNC)
	f.poll(t.Context())
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, "rotated\n", os.O_TRUNC)
	f.poll(t.Context())
	if got := strings.Join(
		logs,
		"|",
	); got != "[access] first|[access] partial completed|[access] short|[access] rotated" {
		t.Fatal(got)
	}
	if grants != 2 {
		t.Fatalf("grants=%d", grants)
	}
}

func TestLogFollowerSkipsHistoryAndDrainsBurst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	writeLog(t, path, "old history\n", os.O_TRUNC)
	c := New(fakeEngine{})
	followers := c.prepareLogFiles(t.Context(), []engine.LogFile{{Path: path, Source: "access"}})
	defer closeLogFiles(followers)
	writeLog(t, path, strings.Repeat("old burst\n", 10000)+"latest\n", os.O_APPEND)
	followers[0].poll(t.Context())
	followers[0].poll(t.Context())
	logs := c.Snapshot().Logs
	if len(logs) > 400 || logs[len(logs)-1] != "[access] latest" ||
		strings.Contains(strings.Join(logs, "\n"), "old history") {
		t.Fatalf("unexpected recent output: %d rows", len(logs))
	}
	if followers[0].offset <= logReadLimit {
		t.Fatal("did not advance past bounded read")
	}
}

func TestLogFollowerCancellationFlushesAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	writeLog(t, path, "", os.O_TRUNC)
	c := New(fakeEngine{})
	files := c.prepareLogFiles(t.Context(), []engine.LogFile{{Path: path, Source: "error"}})
	writeLog(t, path, "final partial", os.O_APPEND)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	followLogFiles(ctx, files)
	if files[0].file != nil {
		t.Fatal("descriptor retained after cancellation")
	}
	if got := strings.Join(c.Snapshot().Logs, "\n"); got != "[error] final partial" {
		t.Fatal(got)
	}
}

func TestLogFollowerRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeLog(t, target, "private\n", os.O_TRUNC)
	path := filepath.Join(dir, "access.log")
	if err := os.Symlink(target, path); err != nil {
		t.Skip(err)
	}
	granted := false
	f := &logFollower{
		config: engine.LogFile{Path: path, Source: "access"},
		log:    func(string) {},
		grant:  func(context.Context, *os.File) error { granted = true; return nil },
	}
	if err := f.open(t.Context(), true); err == nil || granted || f.file != nil {
		t.Fatal("symlink was opened or granted access")
	}
}
