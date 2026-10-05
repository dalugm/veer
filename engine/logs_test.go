package engine

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLogDestinations(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(dir, "error.log")
	for _, tc := range []struct {
		name, access, err string
		want              []LogFile
	}{
		{"console and disabled", "", "none", nil},
		{"relative and absolute", "access.log", absolute, []LogFile{{filepath.Join(dir, "access.log"), "access"}, {absolute, "error"}}},
		{"same destination", "./error.log", absolute, []LogFile{{absolute, "access/error"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := logFiles(dir, tc.access, tc.err); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
