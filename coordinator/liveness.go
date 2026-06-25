package coordinator

import (
	"context"
	"log"
	"math/rand"
	"time"
)

// StartLiveCheckLoop periodically checks which channels are live and updates
// the is_live flag in channel_assignments. Runs every 120 seconds.
// Requires LiveCheck to be set; if nil, this is a no-op.
func (c *Coordinator) StartLiveCheckLoop(ctx context.Context) {
	if !c.IsPooled() || c.Client == nil || c.LiveCheck == nil {
		return
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		// Random initial delay (0-30s) to prevent thundering herd
		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)

		ticker := time.NewTicker(120 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.runLiveCheck()
			}
		}
	}()
}

// runLiveCheck checks all channels in the pool and updates their is_live status.
// Reads directly from channel_assignments (the source of truth in pooled mode).
// Uses a 2-minute timeout to prevent a single stuck API call from hanging
// the goroutine indefinitely. Skips entirely when draining.
func (c *Coordinator) runLiveCheck() {
	if c.LiveCheck == nil {
		return
	}

	// Don't check liveness during draining — the node is shutting down.
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	// Use a timeout so a hung HTTP call cannot leak this goroutine forever.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Read only this node's channel assignments so we don't clobber
	// the is_live status of channels owned by other nodes.
	assignments, err := c.Client.GetNodeAssignments(c.NodeID)
	if err != nil || len(assignments) == 0 {
		return
	}

	// Check liveness for each channel
	var liveUsernames []string
	for _, ca := range assignments {
		if c.LiveCheck.IsLive(ctx, ca.Site, ca.Username) {
			liveUsernames = append(liveUsernames, ca.Username)
		}
	}

	// Bulk-update is_live flags — scoped to this node's channels so we
	// don't race with other nodes' live checks.
	if len(liveUsernames) > 0 {
		if err := c.Client.SetChannelsLive(c.NodeID, liveUsernames); err != nil {
			log.Printf("[coordinator] live check: set live error: %v", err)
		}
		if err := c.Client.SetChannelsNotLive(c.NodeID, liveUsernames); err != nil {
			log.Printf("[coordinator] live check: set not live error: %v", err)
		}
	} else {
		if err := c.Client.SetChannelsNotLive(c.NodeID, []string{}); err != nil {
			log.Printf("[coordinator] live check: set all not live error: %v", err)
		}
	}
}
