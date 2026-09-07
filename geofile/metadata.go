package geofile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const metadataName = ".veer-geo.json"

// FileInfo describes an installed asset. Modified is the filesystem timestamp,
// never a release date. Version is populated only when its saved hash matches.
type FileInfo struct {
	Name     string
	Modified time.Time
	Version  string
	Updated  time.Time
	Source   string
	Err      error
}

type assetMetadata struct {
	Version string    `json:"version,omitempty"`
	Updated time.Time `json:"updated"`
	Source  string    `json:"source"`
	SHA256  string    `json:"sha256"`
}

type releaseInfo struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// latestRelease is optional enrichment: an unavailable API must not prevent
// downloading routing data from the selected mirror.
func latestRelease(ctx context.Context) releaseInfo {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.github.com/repos/Loyalsoldier/v2ray-rules-dat/releases/latest",
		nil,
	)
	if err != nil {
		return releaseInfo{}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Veer")
	resp, err := httpClient.Do(req)
	if err != nil {
		return releaseInfo{}
	}
	defer func() { _ = resp.Body.Close() }()
	var release releaseInfo
	if resp.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release) != nil {
		return releaseInfo{}
	}
	if _, err := time.Parse("200601021504", release.Tag); err != nil {
		return releaseInfo{}
	}
	return release
}

func fileHash(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxGeoFileSize {
		return "", errors.New("invalid geo file")
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		total += n
		if total > maxGeoFileSize {
			return "", errors.New("geo file exceeds size limit")
		}
		_, _ = h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func stageMetadata(
	ctx context.Context,
	dir string,
	names []string,
	source string,
	release releaseInfo,
) error {
	records := make(map[string]assetMetadata, len(names))
	for _, name := range names {
		hash, err := fileHash(ctx, filepath.Join(dir, name))
		if err != nil {
			return err
		}
		record := assetMetadata{Source: source, Updated: time.Now().UTC(), SHA256: hash}
		for _, asset := range release.Assets {
			if asset.Name == name && asset.Digest == "sha256:"+hash {
				record.Version = release.Tag
			}
		}
		records[name] = record
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, metadataName), data, 0o644)
}

// Inspect reads local metadata without accessing the network. Existing files
// without usable metadata retain their filesystem modification timestamp.
func Inspect(ctx context.Context, dir string) []FileInfo {
	records := map[string]assetMetadata{}
	metadataPath := filepath.Join(dir, metadataName)
	if info, err := os.Stat(
		metadataPath,
	); err == nil && info.Mode().IsRegular() &&
		info.Size() <= 64<<10 {
		if f, err := os.Open(metadataPath); err == nil {
			if json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&records) != nil {
				records = nil
			}
			_ = f.Close()
		}
	}
	result := make([]FileInfo, 0, 2)
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		item := FileInfo{Name: name}
		info, err := os.Stat(filepath.Join(dir, name))
		item.Err = err
		if err == nil {
			if !info.Mode().IsRegular() {
				item.Err = errors.New("not a regular file")
			} else {
				item.Modified = info.ModTime()
				if record, ok := records[name]; ok && len(record.SHA256) == 64 {
					hash, err := fileHash(ctx, filepath.Join(dir, name))
					if err == nil && hash == record.SHA256 {
						item.Updated, item.Source = record.Updated, record.Source
						if _, err := time.Parse(
							"200601021504",
							record.Version,
						); err == nil {
							item.Version = record.Version
						}
					}
				}
			}
		}
		result = append(result, item)
	}
	return result
}
