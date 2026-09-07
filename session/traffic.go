package session

import (
	"context"
	"time"

	"github.com/dalugm/veer/engine"
)

// pollTraffic has the same lifetime as its core; the process waiter joins it
// before releasing the session or deleting the temporary configuration.
func (c *Controller) pollTraffic(ctx context.Context, plan engine.Plan) {
	if plan.StatsAddress == "" {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		sample, err := c.sampleTraffic(ctx, plan.Binary, plan.StatsAddress)
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		if err != nil {
			c.snapshot.TrafficError = "Traffic statistics unavailable; retrying"
		} else {
			c.snapshot.Traffic = sample
			c.snapshot.TrafficError = ""
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
