package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionArchiveSearchIncludesEvictedEntries(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "session.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	c := New(fakeEngine{}, WithLogArchive(file))
	c.Log("old unique needle\x1b[31m")
	for i := range 1000 {
		c.Log(fmt.Sprintf("recent %d", i))
	}
	if strings.Contains(strings.Join(c.Snapshot().Logs, "\n"), "needle") {
		t.Fatal("fixture did not evict old entry")
	}
	result, err := SearchLogs(t.Context(), file.Name(), "NEEDLE", 0)
	if err != nil || !reflect.DeepEqual(result.Lines, []string{"old unique needle"}) ||
		result.Matches != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if c.Snapshot().LogCount != 1001 {
		t.Fatal("incorrect archive count")
	}
}

func TestArchiveSearchPaginationAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.log")
	var data strings.Builder
	for i := range 1001 {
		fmt.Fprintf(&data, "match %04d\n", i)
	}
	if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ skip, first, last, length int }{{0, 601, 1000, 400}, {400, 201, 600, 400}, {800, 0, 200, 201}, {-1, 0, 200, 201}} {
		result, err := SearchLogs(t.Context(), path, "match", tc.skip)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Lines) != tc.length ||
			result.Lines[0] != fmt.Sprintf("match %04d", tc.first) ||
			result.Lines[len(result.Lines)-1] != fmt.Sprintf("match %04d", tc.last) ||
			result.Matches != 1001 {
			t.Fatalf("skip %d: %+v", tc.skip, result)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := SearchLogs(ctx, path, "match", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
