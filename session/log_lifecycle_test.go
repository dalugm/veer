package session

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
)

type fileLogEngine struct{ path string }

func (e fileLogEngine) Prepare(o engine.Options) (engine.Plan, error) {
	p, err := (fakeEngine{"run"}).Prepare(o)
	p.Info = engine.Info{LogFiles: []engine.LogFile{{Path: e.path, Source: "error"}}}
	p.Args = []string{"-test.run=TestFileLoggingChild"}
	p.Env = append(p.Env, "VEER_TEST_FILE_LOG="+e.path)
	return p, err
}

func TestFileLoggingChild(t *testing.T) {
	path := os.Getenv("VEER_TEST_FILE_LOG")
	if path == "" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(3)
	}
	fmt.Println("console output")
	_, _ = f.WriteString("launch file output\n")
	<-signals
	_, _ = f.WriteString("shutdown file output\n")
	_ = f.Close()
	os.Exit(0)
}

func TestControllerFollowsFileThroughChildShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows child termination does not send os.Interrupt")
	}
	path := filepath.Join(t.TempDir(), "error.log")
	c := New(fileLogEngine{path: path})
	if err := c.Start(t.Context(), engine.Options{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	logs := strings.Join(c.Snapshot().Logs, "\n")
	for _, want := range []string{"console output", "[error] launch file output", "[error] shutdown file output"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing %q in %s", want, logs)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "shutdown file output") {
		t.Fatalf("original file not preserved: %q %v", data, err)
	}
}

func TestReconnectStartsNewLogSession(t *testing.T) {
	c := New(fakeEngine{"run"})
	if err := c.Start(t.Context(), engine.Options{}); err != nil {
		t.Fatal(err)
	}
	c.Log("previous-session-marker")
	if err := c.Start(t.Context(), engine.Options{}); err == nil {
		t.Fatal("duplicate start accepted")
	}
	if !strings.Contains(strings.Join(c.Snapshot().Logs, "\n"), "previous-session-marker") {
		t.Fatal("rejected start cleared active logs")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(c.Snapshot().Logs, "\n"), "previous-session-marker") {
		t.Fatal("stop cleared diagnostic logs")
	}
	if err := c.Start(t.Context(), engine.Options{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Stop(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	if strings.Contains(strings.Join(c.Snapshot().Logs, "\n"), "previous-session-marker") {
		t.Fatal("reconnect retained previous session")
	}
}
