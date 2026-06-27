package coordinator

import (
	"context"
	"hash/fnv"
	"log"
	"math/rand"
	"time"
)

// StartOfflineRotationLoop periodically releases a random subset of non-live
// channels back to the pool so other nodes get a chance to claim and record
// them. Runs every 5 minutes with ±30s random jitter per cycle.
//
// Rationale: a node may be alive and heartbeating but unable to record certain
// channels due to node-local issues (age verification, Cloudflare blocking,
// geo-restrictions, stale cookies, etc.).  Without rotation those channels are
// stuck on the sick node until the reaper decides it's dead (180s heartbeat
// timeout), and even then only if the node fully crashes.  By proactively
// releasing a subset of offline channels every cycle we give every node a
// chance to try recording them.
func (c *Coordinator) StartOfflineRotationLoop(ctx context.Context) {
	if !c.IsPooled() || c.Client == nil {
		return
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		// Stagger initial delay by node-ID hash so nodes don't all rotate
		// their channels at the same wall-clock time.
		h := fnv.New32a()
		h.Write([]byte(c.NodeID))
		stagger := 30*time.Second + time.Duration(h.Sum32()%60)*time.Second
		time.Sleep(stagger)

		for {
			jitter := time.Duration(rand.Intn(61)-30) * time.Second
			interval := 5*time.Minute + jitter
			if interval <= 0 {
				interval = 5 * time.Minute
			}
			timer := time.NewTimer(interval)

			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-c.stopCh:
				timer.Stop()
				return
			case <-timer.C:
				c.runOfflineRotation()
			}
		}
	}()
}

// runOfflineRotation executes one iteration of the offline-channel rotation.
// It picks a random subset of this node's non-live channels and releases them
// back to the pool so other nodes can try to claim and record them.
func (c *Coordinator) runOfflineRotation() {
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	// Fetch all assignments for this node.
	assignments, err := c.Client.GetNodeAssignments(c.NodeID)
	if err != nil {
		log.Printf("[coordinator] offline rotation: get assignments error: %v", err)
		return
	}

	// Collect non-live channel usernames.
	var offline []string
	for _, a := range assignments {
		if !a.IsLive && a.Status != "unassigned" {
			offline = append(offline, a.Username)
		}
	}

	if len(offline) == 0 {
		return
	}

	// Pick a random subset: at least 1, at most ceil(25%) of offline channels.
	subsetSize := pickRotationSubsetSize(len(offline))

	// Shuffle and pick the first subsetSize channels.
	rand.Shuffle(len(offline), func(i, j int) {
		offline[i], offline[j] = offline[j], offline[i]
	})
	toRelease := offline[:subsetSize]

	// Release them in the database.
	released, err := c.Client.ReleaseChannelsByUsername(c.NodeID, toRelease)
	if err != nil {
		log.Printf("[coordinator] offline rotation: release error: %v", err)
		return
	}

	if len(released) == 0 {
		return
	}

	log.Printf("[coordinator] offline rotation: released %d channel(s) (had %d offline, picked %d)",
		len(released), len(offline), subsetSize)

	// Stop local recording for each released channel so the local Manager
	// does not continue recording channels we no longer own.
	if c.Manager != nil {
		for _, ca := range released {
			c.Manager.RemoveChannelForReassignment(ca.Username)
		}
	}
}

// pickRotationSubsetSize returns the number of channels to release in one
// rotation cycle given the count of offline channels.  At least 1, at most
// ceil(25%) of the total.  Exported for testing.
func pickRotationSubsetSize(offlineCount int) int {
	if offlineCount <= 0 {
		return 0
	}
	n := (offlineCount + 3) / 4 // ceil(offlineCount / 4)
	if n < 1 {
		n = 1
	}
	if n > offlineCount {
		n = offlineCount
	}
	return n
}
