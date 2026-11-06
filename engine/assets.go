package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
)

// GeoAssetDirectories locates existing Xray assets for one-time initialization.
// Runtime sessions still use Veer's single managed directory.
func GeoAssetDirectories(binary string) []string {
	var dirs []string
	if dir := os.Getenv("XRAY_LOCATION_ASSET"); dir != "" {
		dirs = append(dirs, dir)
	}
	if path, err := exec.LookPath(binary); err == nil {
		if path, err = filepath.Abs(path); err == nil {
			dirs = append(dirs, filepath.Dir(path))
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				dirs = append(dirs, filepath.Dir(resolved))
			}
		}
	}
	if runtime.GOOS != "windows" {
		dirs = append(dirs, "/usr/local/share/xray", "/usr/share/xray")
	}
	return slices.Compact(dirs)
}
