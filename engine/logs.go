package engine

import "path/filepath"

// LogFile is a configured file destination, resolved against the core's working
// directory. Console and disabled destinations are excluded.
type LogFile struct {
	Path, Source string
}

func logFiles(dir, access, errorPath string) []LogFile {
	var files []LogFile
	for _, item := range []LogFile{{access, "access"}, {errorPath, "error"}} {
		if item.Path == "" || item.Path == "none" {
			continue
		}
		if !filepath.IsAbs(item.Path) {
			item.Path = filepath.Join(dir, item.Path)
		}
		item.Path = filepath.Clean(item.Path)
		if len(files) > 0 && files[0].Path == item.Path {
			files[0].Source = "access/error"
			continue
		}
		files = append(files, item)
	}
	return files
}
