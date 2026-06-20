package coordinator

import (
	"context"
	"log"
	"time"
)

// StartHeartbeatLoop periodically updates the node's last_heartbeat timestamp.
// Runs every 30 seconds until the context is cancelled or Stop() is called.
func (c *Coordinator) StartHeartbeatLoop(ctx context.Context) {
	if !c.IsPooled() || c.Client == nil {
		return
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				load := c.currentLoad()
				if err := c.Client.HeartbeatNode(c.NodeID, load); err != nil {
					log.Printf("[coordinator] heartbeat error: %v", err)
				}
			}
		}
	}()
}
