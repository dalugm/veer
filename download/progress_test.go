package download

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestTrackStreamsKnownAndUnknownSizes(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"known", 8}, {"unknown", -1}} {
		t.Run(tc.name, func(t *testing.T) {
			var updates []Progress
			reader := Track(
				iotest.OneByteReader(strings.NewReader("abcdefgh")),
				"asset",
				tc.size,
				func(p Progress) {
					updates = append(updates, p)
				},
			)
			var output bytes.Buffer
			if _, err := io.Copy(&output, reader); err != nil || output.String() != "abcdefgh" {
				t.Fatalf("stream changed: %q, %v", output.String(), err)
			}
			if updates[0].Received != 0 || updates[0].Done {
				t.Fatalf("invalid initial progress: %+v", updates[0])
			}
			last := updates[len(updates)-1]
			if last.Name != "asset" || last.Received != 8 || last.Total != max(0, tc.size) ||
				!last.Done {
				t.Fatalf("invalid final progress: %+v", last)
			}
			for i, p := range updates[1:] {
				if p.Received < updates[i].Received || (p.Done && p.Received != 8) {
					t.Fatalf("invalid intermediate progress: %+v", updates)
				}
			}
		})
	}
}

func TestTrackPreservesReadFailure(t *testing.T) {
	want := errors.New("download interrupted")
	var last Progress
	reader := Track(iotest.ErrReader(want), "asset", 8, func(p Progress) { last = p })
	if _, err := io.Copy(io.Discard, reader); !errors.Is(err, want) {
		t.Fatalf("read error = %v", err)
	}
	if last.Done || last.Received != 0 {
		t.Fatalf("failed download reported completion: %+v", last)
	}
}
