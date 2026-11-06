package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestGeoAssetDirectoriesFindsInstallation(t *testing.T) {
	dir := t.TempDir()
	name := "xray"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	if err := os.WriteFile(binary, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("XRAY_LOCATION_ASSET", filepath.Join(dir, "assets"))
	dirs := GeoAssetDirectories(name)
	if len(dirs) == 0 || dirs[0] != filepath.Join(dir, "assets") || !slices.Contains(dirs, dir) {
		t.Fatalf("directories: %v", dirs)
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(t.TempDir(), "xray")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirs := GeoAssetDirectories(
		link,
	); !slices.Contains(dirs, resolved) ||
		!slices.Contains(dirs, filepath.Dir(link)) {
		t.Fatalf("symlink assets missing: %v", dirs)
	}
}
