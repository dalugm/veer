package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/dalugm/veer/engine"
)

// Start validates and starts the core, waiting for startup or cancellation.
func (c *Controller) Start(parent context.Context, o engine.Options) error {
	c.mu.Lock()
	if c.cancel != nil || c.recovering != nil {
		c.mu.Unlock()
		return errors.New("stop the current session before connecting")
	}
	if c.cleanupErr != nil {
		err := fmt.Errorf("restore the previous session before connecting: %w", c.cleanupErr)
		c.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.snapshot.State = Validating
	c.snapshot.Error = ""
	c.cleanupErr = nil
	c.snapshot.Ready = false
	c.snapshot.PID = 0
	c.snapshot.Endpoints = nil
	c.snapshot.Traffic = engine.Traffic{}
	c.snapshot.TrafficError = ""
	c.mu.Unlock()
	var connectionNetwork networkState
	runtimeCleanup := func() error { return nil }
	restore := func(ctx context.Context) error {
		err := connectionNetwork.restore(ctx)
		if err != nil {
			err = errors.Join(errCleanup, err)
		}
		if e := runtimeCleanup(); e != nil {
			err = errors.Join(err, errCleanup, errors.New("remove temporary Xray configuration"), e)
		}
		return err
	}
	c.mu.Lock()
	c.cleanup = restore
	c.mu.Unlock()
	cleanup := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return restore(ctx)
	}
	fail := func(err error) error {
		err = errors.Join(err, cleanup())
		c.finish(ctx, err, done)
		cancel()
		return err
	}
	plan, err := c.adapter.Prepare(o)
	if err != nil {
		return fail(err)
	}
	if adapter, ok := c.adapter.(interface {
		PrepareRuntime(engine.Plan) (engine.Plan, func() error, error)
	}); ok {
		var remove func() error
		plan, remove, err = adapter.PrepareRuntime(plan)
		if remove != nil {
			runtimeCleanup = remove
		}
		if err != nil {
			return fail(err)
		}
		if plan.StatsAddress == "" {
			c.mu.Lock()
			c.snapshot.TrafficError = "Traffic statistics unavailable for this API configuration"
			c.mu.Unlock()
		}
	}
	if err := c.validatePlan(ctx, plan); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if err := c.prepareNetwork(ctx, plan.Info, o, &connectionNetwork); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	cmd := exec.Command(plan.Binary, plan.Args...)
	cmd.Dir = plan.Dir
	cmd.Env = append(os.Environ(), plan.Env...)
	configureProcess(cmd)
	output := &logWriter{controller: c}
	cmd.Stdout = output
	cmd.Stderr = output
	if err = cmd.Start(); err != nil {
		return fail(fmt.Errorf("start Xray: %w", err))
	}
	c.mu.Lock()
	c.snapshot.State = Starting
	c.snapshot.PID = cmd.Process.Pid
	c.snapshot.Since = time.Now()
	c.snapshot.Endpoints = append([]string(nil), plan.Info.Endpoints...)
	c.mu.Unlock()
	c.Log(fmt.Sprintf("Xray started · PID %d", cmd.Process.Pid))
	samplesDone := make(chan struct{})
	go func() { defer close(samplesDone); c.pollTraffic(ctx, plan) }()
	go func() {
		err := cmd.Wait()
		// Cancel polling/readiness before cleanup. The Wait result still determines
		// whether an unsolicited engine exit is a failure.
		unexpected := ctx.Err() == nil
		cancel()
		<-samplesDone
		output.Flush()
		err = errors.Join(err, cleanup())
		finishCtx := ctx
		if unexpected {
			finishCtx = context.Background()
		}
		c.finish(finishCtx, err, done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = interrupt(cmd)
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				_ = cmd.Process.Kill()
			}
		case <-done:
		}
	}()
	readyCtx, readyCancel := context.WithTimeout(ctx, 15*time.Second)
	defer readyCancel()
	startFailed := func(err error) error {
		cancel()
		<-done
		c.mu.Lock()
		defer c.mu.Unlock()
		// A caller may reconnect after Stop returns while this Start is still
		// unwinding. Never publish the old failure into the new session.
		if c.done != done {
			return err
		}
		err = errors.Join(err, c.cleanupErr)
		if parent.Err() == nil || c.cleanupErr != nil {
			c.snapshot.State = Failed
			c.snapshot.Error = err.Error()
		}
		return err
	}
	if err := connectionNetwork.readyTUN(readyCtx, c.Log); err != nil {
		return startFailed(err)
	}
	started := time.Now()
	for {
		select {
		case <-done:
			s := c.Snapshot()
			if s.Error != "" {
				return errors.New(s.Error)
			}
			return errors.New("xray exited during startup")
		default:
		}
		ready := len(plan.Info.Endpoints) > 0
		for _, address := range plan.Info.Endpoints {
			conn, e := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(
				readyCtx,
				"tcp",
				address,
			)
			if e != nil {
				ready = false
				break
			}
			_ = conn.Close()
		}
		if ready || (len(plan.Info.Endpoints) == 0 && time.Since(started) >= 400*time.Millisecond) {
			if err := connectionNetwork.applyProxy(readyCtx, c.Log); err != nil {
				return startFailed(err)
			}
			c.mu.Lock()
			if c.done == done && c.snapshot.State == Starting {
				c.snapshot.State = Running
				c.snapshot.Ready = ready
				c.mu.Unlock()
				if ready {
					c.Log("Local proxy listener is ready")
				} else {
					c.Log("Process running; this config has no probeable local proxy listener")
				}
				return nil
			}
			c.mu.Unlock()
		}
		select {
		case <-done:
			continue
		case <-readyCtx.Done():
			err := readyCtx.Err()
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("local proxy listener did not become ready within 15 seconds")
			}
			return startFailed(err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
