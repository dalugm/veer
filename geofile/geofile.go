// Package geofile downloads and validates Xray routing data files.
package geofile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"time"
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

func extractFilenameFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	filename := path.Base(u.Path)
	if filename == "." || filename == "/" || filename == ".." || filename == "" {
		return "", errors.New("URL does not contain a safe filename")
	}

	return filename, nil
}

func downloadFile(ctx context.Context, urlStr, fullPath string) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), "."+filepath.Base(fullPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "go-geo-updater/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_ = tmpFile.Close()
		return fmt.Errorf("http status: %s", resp.Status)
	}

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxGeoFileSize+1))
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("save content: %w", err)
	}
	if written > maxGeoFileSize {
		_ = tmpFile.Close()
		return fmt.Errorf("download exceeds %d bytes", maxGeoFileSize)
	}
	if written < minGeoFileSize {
		_ = tmpFile.Close()
		return fmt.Errorf("download is unexpectedly small: %d bytes", written)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("set file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return fmt.Errorf("publish staged file: %w", err)
	}
	return nil
}

func downloadAll(ctx context.Context, urls []string, stageDir string) ([]string, []error) {
	filenames := make([]string, len(urls))
	results := make([]error, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for i, rawURL := range urls {
		filename, err := extractFilenameFromURL(rawURL)
		if err != nil {
			results[i] = fmt.Errorf("%s: %w", rawURL, err)
			continue
		}
		if _, exists := seen[filename]; exists {
			results[i] = fmt.Errorf("duplicate output filename %q", filename)
			continue
		}
		seen[filename] = struct{}{}
		filenames[i] = filename
	}

	var wg sync.WaitGroup
	for i, rawURL := range urls {
		if results[i] != nil {
			continue
		}
		wg.Add(1)
		go func(index int, urlStr, filename string) {
			defer wg.Done()
			if err := downloadFile(ctx, urlStr, filepath.Join(stageDir, filename)); err != nil {
				results[index] = fmt.Errorf("%s: %w", urlStr, err)
			}
		}(i, rawURL, filenames[i])
	}
	wg.Wait()

	errs := make([]error, 0)
	for _, err := range results {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return filenames, errs
}

type publishState struct {
	filename    string
	target      string
	backup      string
	hadOriginal bool
	installed   bool
}

func publishStagedFiles(stageDir, destination string, filenames []string) error {
	backupDir := filepath.Join(stageDir, ".backup")
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}

	states := make([]publishState, len(filenames))
	for i, filename := range filenames {
		stagedPath := filepath.Join(stageDir, filename)
		stagedInfo, err := os.Stat(stagedPath)
		if err != nil {
			return fmt.Errorf("inspect staged %s: %w", filename, err)
		}
		if !stagedInfo.Mode().IsRegular() {
			return fmt.Errorf("staged %s is not a regular file", filename)
		}

		state := publishState{
			filename: filename,
			target:   filepath.Join(destination, filename),
			backup:   filepath.Join(backupDir, filename),
		}
		targetInfo, err := os.Lstat(state.target)
		switch {
		case err == nil:
			if !targetInfo.Mode().IsRegular() {
				return fmt.Errorf("destination %s is not a regular file", state.target)
			}
			state.hadOriginal = true
			if err := os.Chmod(stagedPath, targetInfo.Mode().Perm()); err != nil {
				return fmt.Errorf("preserve permissions for %s: %w", filename, err)
			}
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return fmt.Errorf("inspect destination %s: %w", state.target, err)
		}
		states[i] = state
	}

	rollback := func() error {
		var rollbackErrors []error
		for _, state := range slices.Backward(states) {

			if state.installed {
				if err := os.Remove(state.target); err != nil && !errors.Is(err, os.ErrNotExist) {
					rollbackErrors = append(rollbackErrors, err)
				}
			}
			if state.hadOriginal {
				if _, err := os.Stat(state.backup); err == nil {
					if err := os.Rename(state.backup, state.target); err != nil {
						rollbackErrors = append(rollbackErrors, err)
					}
				}
			}
		}
		return errors.Join(rollbackErrors...)
	}

	for i := range states {
		if !states[i].hadOriginal {
			continue
		}
		if err := os.Rename(states[i].target, states[i].backup); err != nil {
			return errors.Join(fmt.Errorf("back up %s: %w", states[i].filename, err), rollback())
		}
	}
	for i := range states {
		if err := os.Rename(
			filepath.Join(stageDir, states[i].filename),
			states[i].target,
		); err != nil {
			return errors.Join(fmt.Errorf("publish %s: %w", states[i].filename, err), rollback())
		}
		states[i].installed = true
	}
	return nil
}

func updateGeoFiles(ctx context.Context, urls []string, savePath string) error {
	return updateGeoFilesWithMetadata(ctx, urls, savePath, "", releaseInfo{})
}

func updateGeoFilesWithMetadata(
	ctx context.Context,
	urls []string,
	savePath, source string,
	release releaseInfo,
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
	defer func() { _ = os.RemoveAll(stageDir) }()
	if err := os.Chmod(stageDir, 0o700); err != nil {
		return fmt.Errorf("secure staging directory: %w", err)
	}

	filenames, downloadErrors := downloadAll(ctx, urls, stageDir)
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
		return err
	}
	return nil
}

// Update downloads and atomically publishes geoip.dat and geosite.dat.
func Update(ctx context.Context, source, savePath string) error {
	urls, ok := geoFileSources[source]
	if !ok {
		return fmt.Errorf("unknown source: %s (available: github, cdn, fastly)", source)
	}
	release := latestRelease(ctx)
	if err := updateGeoFilesWithMetadata(ctx, urls, savePath, source, release); err != nil {
		return fmt.Errorf("geo file update failed; existing files were preserved: %w", err)
	}
	return nil
}
