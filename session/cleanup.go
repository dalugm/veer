package session

import "context"

// Retain the original restoration operation until it succeeds. Snapshot reads
// remain available while OS commands run, and concurrent retries share a result.
func (c *Controller) retryCleanup(ctx context.Context) error {
	c.mu.Lock()
	if waiting := c.recovering; waiting != nil {
		c.mu.Unlock()
		select {
		case <-waiting:
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.cleanupErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if c.cleanupErr == nil || c.cleanup == nil {
		err := c.cleanupErr
		c.mu.Unlock()
		return err
	}
	cleanup := c.cleanup
	c.recovering = make(chan struct{})
	c.snapshot.State = Stopping
	c.mu.Unlock()

	err := cleanup(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupErr = err
	c.snapshot.CleanupPending = err != nil
	if err == nil {
		c.cleanup = nil
		c.snapshot.State = Stopped
		c.snapshot.Error = ""
	} else {
		c.snapshot.State = Failed
		c.snapshot.Error = err.Error()
	}
	close(c.recovering)
	c.recovering = nil
	return err
}
