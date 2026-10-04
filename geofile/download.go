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
	"sync"

	"github.com/dalugm/veer/engine"
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
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read staged geo data: %w", err)
	}
	if err := engine.ValidateGeoData(ctx, filepath.Base(fullPath), data); err != nil {
		return err
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
