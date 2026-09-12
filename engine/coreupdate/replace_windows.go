package coreupdate

import (
	"context"
	"os"
)

func replaceExecutable(ctx context.Context, staged, target, backup string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Windows cannot overwrite a running executable, but can rename it before
	// publishing the new image. No helper process or shell script is needed.
	return replaceWithBackup(staged, target, backup, os.Rename)
}
