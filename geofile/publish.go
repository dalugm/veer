package geofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

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
				} else if !errors.Is(err, os.ErrNotExist) {
					rollbackErrors = append(rollbackErrors, err)
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
