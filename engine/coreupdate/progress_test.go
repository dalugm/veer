package coreupdate

import (
	"context"
	"errors"
	"testing"

	"github.com/dalugm/veer/download"
)

func TestInstallReportsArchiveProgress(t *testing.T) {
	c, release, target := installFixture(t)
	var updates []download.Progress
	if _, err := c.Install(t.Context(), target, "v1.0.0", release, func(p download.Progress) {
		updates = append(updates, p)
	}); err != nil {
		t.Fatal(err)
	}
	if len(updates) < 2 || updates[0].Received != 0 || updates[0].Total != release.binary.Size {
		t.Fatalf("missing initial archive size: %+v", updates)
	}
	last := updates[len(updates)-1]
	if last.Name != "Xray" || last.Received != release.binary.Size || !last.Done {
		t.Fatalf("archive progress did not finish: %+v", last)
	}
}

func TestCancelFromProgressPreservesInstalledCore(t *testing.T) {
	c, release, target := installFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := c.Install(ctx, target, "v1.0.0", release, func(p download.Progress) {
		if p.Received > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled install = %v", err)
	}
	assertContents(t, target, "old executable")
}
