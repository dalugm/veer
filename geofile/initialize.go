package geofile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dalugm/veer/engine"
)

// Initialize copies validated local assets into missing managed slots. Call only
// after an explicit user action. It neither downloads nor replaces existing data.
// Unavailable sources are skipped, leaving the normal download action available.
func Initialize(ctx context.Context, destination string, sources []string) error {
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(destination, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, dir := range sources {
			data, err := readLocalGeo(ctx, filepath.Join(dir, name), name)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			if err := publishInitialGeo(ctx, target, data); err != nil {
				return fmt.Errorf("initialize %s: %w", name, err)
			}
			break
		}
	}
	return ctx.Err()
}

func readLocalGeo(ctx context.Context, path, name string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxGeoFileSize {
		return nil, errors.New("invalid local geo file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxGeoFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGeoFileSize {
		return nil, errors.New("local geo file exceeds size limit")
	}
	if err := engine.ValidateGeoData(ctx, name, data); err != nil {
		return nil, err
	}
	return data, nil
}

func publishInitialGeo(ctx context.Context, target string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".geo-init-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Publish the complete file without replacing a concurrent writer's asset.
	// Both paths are on the destination filesystem, including on Windows.
	if err := os.Link(f.Name(), target); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}
