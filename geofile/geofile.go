// Package geofile downloads and validates Xray routing data files.
package geofile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dalugm/veer/download"
)

const (
	minGeoFileSize = 1024
	maxGeoFileSize = 256 << 20
)

var (
	geoFileSources = map[string][]string{
		"github": {
			"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
			"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
		},
		"cdn": {
			"https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geoip.dat",
			"https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
		},
		"fastly": {
			"https://fastly.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geoip.dat",
			"https://fastly.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
		},
	}

	httpClient = &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
)

func updateGeoFiles(ctx context.Context, urls []string, savePath string) error {
	return updateGeoFilesWithMetadata(ctx, urls, savePath, "", releaseInfo{}, nil)
}

func updateGeoFilesWithMetadata(
	ctx context.Context,
	urls []string,
	savePath, source string,
	release releaseInfo,
	report func(download.Progress),
) error {
	destination := savePath
	if destination == "" {
		destination = "."
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	stageDir, err := os.MkdirTemp(destination, ".geofile-stage-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stageDir)
		}
	}()
	if err := os.Chmod(stageDir, 0o700); err != nil {
		return fmt.Errorf("secure staging directory: %w", err)
	}

	filenames, downloadErrors := downloadAll(ctx, urls, stageDir, report)
	if len(downloadErrors) > 0 {
		return errors.Join(downloadErrors...)
	}
	if err := stageMetadata(ctx, stageDir, filenames, source, release); err != nil {
		return fmt.Errorf("save geo metadata: %w", err)
	}
	filenames = append(filenames, metadataName)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishStagedFiles(stageDir, destination, filenames); err != nil {
		backups, readErr := os.ReadDir(filepath.Join(stageDir, ".backup"))
		if len(backups) > 0 || (readErr != nil && !errors.Is(readErr, os.ErrNotExist)) {
			keepStage = true
			return fmt.Errorf(
				"geo publication failed; recovery files kept in %s: %w",
				stageDir,
				err,
			)
		}
		return err
	}
	return nil
}

// Update downloads and atomically publishes geoip.dat and geosite.dat.
// report optionally receives per-file download progress and must be safe for
// concurrent calls from the two download workers.
func Update(ctx context.Context, source, savePath string, report func(download.Progress)) error {
	urls, ok := geoFileSources[source]
	if !ok {
		return fmt.Errorf("unknown source: %s (available: github, cdn, fastly)", source)
	}
	release := latestRelease(ctx)
	if err := updateGeoFilesWithMetadata(ctx, urls, savePath, source, release, report); err != nil {
		return fmt.Errorf("geo file update failed: %w", err)
	}
	return nil
}
