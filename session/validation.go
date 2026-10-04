package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/dalugm/veer/engine"
)

func (c *Controller) validatePlan(ctx context.Context, plan engine.Plan) error {
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
