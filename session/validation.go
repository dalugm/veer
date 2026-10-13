package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/dalugm/veer/engine"
)

func (c *Controller) validatePlan(ctx context.Context, plan engine.Plan) error {
	if required, _ := plan.Info.RequiredVersion(runtime.GOOS); required != "" {
		version, err := engine.Version(ctx, plan.Binary)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("verify required Xray version %s: %w", required, err)
		}
		if err := plan.Info.CheckVersion(version, runtime.GOOS); err != nil {
			return err
		}
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	check := exec.CommandContext(checkCtx, plan.Binary, plan.CheckArgs...)
	check.Dir = plan.Dir
	check.Env = append(os.Environ(), plan.Env...)
	configureProcess(check)
	checkLog := &logWriter{controller: c}
	check.Stdout = checkLog
	check.Stderr = checkLog
	err := check.Run()
	checkLog.Flush()
	checkCancel()
	if err != nil {
		return fmt.Errorf("xray configuration validation failed: %w (see Logs)", err)
	}
	return nil
}
