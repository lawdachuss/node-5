//go:build integration

package coordinator

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/teacat/chaturbate-dvr/database"
	"github.com/teacat/chaturbate-dvr/entity"
)

// These integration tests require a real Supabase project.
// Run with: go test -tags=integration -run TestIntegration ./coordinator/
// Set SUPABASE_URL and SUPABASE_API_KEY env vars to point to a test Supabase project.

func skipIfNoSupabase(t *testing.T) *database.Client {
	t.Helper()
	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_API_KEY")
	if url == "" || key == "" {
		t.Skip("Skipping integration test: set SUPABASE_URL and SUPABASE_API_KEY")
	}
	return database.NewClient(url, key)
}

const testNodeID = "coordinator-int-test-node"

func cleanupAssignments(t *testing.T, client *database.Client, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_ = client.ReleaseNodeChannels(id)
	}
	// Also clean up any assignments created during the test
	if assignments, err := client.GetAllAssignments(); err == nil {
		for _, a := range assignments {
			if a.AssignedNode == testNodeID || a.Username == testNodeID {
				_ = client.DeleteAssignment(a.Username, a.Site)
			}
		}
	}
}

func cleanupNode(t *testing.T, client *database.Client) {
	t.Helper()
	_ = client.DeleteAssignment(testNodeID, "chaturbate")
	_ = client.DeleteAssignment(testNodeID, "stripchat")
	// Mark node offline (can't delete — FK reference)
	_ = client.UpdateNodeStatus(testNodeID, "offline")
}

// TestIntegrationRegisterAndHeartbeat verifies node registration and heartbeat.
func TestIntegrationRegisterAndHeartbeat(t *testing.T) {
	client := skipIfNoSupabase(t)

	coord := New(client, &mockChannelManager{})
	coord.NodeID = testNodeID
	coord.Mode = entity.PoolModePooled
	defer cleanupNode(t, client)

	// Register
	coord.Register()

	// Verify node exists
	node, err := client.GetNode(testNodeID)
	if err != nil {
		t.Fatalf("GetNode error: %v", err)
	}
	if node.Status != "online" {
		t.Errorf("node status = %q, want online", node.Status)
	}

	// Heartbeat
	err = client.HeartbeatNode(testNodeID, 3)
	if err != nil {
		t.Fatalf("HeartbeatNode error: %v", err)
	}

	// Verify heartbeat updated
	node, err = client.GetNode(testNodeID)
	if err != nil {
		t.Fatalf("GetNode error: %v", err)
	}
	if node.CurrentLoad != 3 {
		t.Errorf("current_load = %d, want 3", node.CurrentLoad)
	}
}

// TestIntegrationClaimChannels verifies atomic channel claiming.
func TestIntegrationClaimChannels(t *testing.T) {
	client := skipIfNoSupabase(t)
	defer cleanupAssignments(t, client, testNodeID)
	defer cleanupNode(t, client)

	// Register node
	coord := New(client, &mockChannelManager{})
	coord.NodeID = testNodeID
	coord.Mode = entity.PoolModePooled
	coord.Register()

	// Create test assignments for this node and make them live unassigned
	assignments := []database.ChannelAssignment{
		{Username: testNodeID + "-ch1", Site: "chaturbate", Status: "unassigned", IsLive: true, Framerate: 60, Resolution: 1080},
		{Username: testNodeID + "-ch2", Site: "stripchat", Status: "unassigned", IsLive: true, Framerate: 30, Resolution: 720},
		{Username: testNodeID + "-ch3", Site: "chaturbate", Status: "unassigned", IsLive: false, Framerate: 60, Resolution: 2160},
	}
	if err := client.BulkInsertAssignments(assignments); err != nil {
		t.Fatalf("BulkInsertAssignments error: %v", err)
	}
	defer func() {
		for _, a := range assignments {
			_ = client.DeleteAssignment(a.Username, a.Site)
		}
	}()

	// Claim up to 2 live channels
	claimed, err := client.ClaimChannels(testNodeID, 2)
	if err != nil {
		t.Fatalf("ClaimChannels error: %v", err)
	}
	if len(claimed) != 2 {
		t.Errorf("ClaimChannels returned %d, want 2 (should claim the 2 live channels)", len(claimed))
	}

	// Verify claimed channels are assigned to our node
	for _, c := range claimed {
		if c.AssignedNode != testNodeID {
			t.Errorf("claimed %s assigned to %q, want %q", c.Username, c.AssignedNode, testNodeID)
		}
		if c.Status != "claimed" {
			t.Errorf("claimed %s status = %q, want claimed", c.Username, c.Status)
		}
	}

	// Verify the offline channel was NOT claimed
	offline, err := client.GetAssignment(testNodeID+"-ch3", "chaturbate")
	if err != nil {
		t.Fatalf("GetAssignment error: %v", err)
	}
	if offline != nil && offline.AssignedNode != "" {
		t.Error("offline channel should not have been claimed")
	}
}

// TestIntegrationFairShareAcrossNodes verifies that multiple nodes get a fair share.
func TestIntegrationFairShareAcrossNodes(t *testing.T) {
	client := skipIfNoSupabase(t)

	nodeA := testNodeID + "-a"
	nodeB := testNodeID + "-b"

	// Register both nodes
	for _, id := range []string{nodeA, nodeB} {
		coord := New(client, &mockChannelManager{})
		coord.NodeID = id
		coord.Mode = entity.PoolModePooled
		coord.Register()
	}
	defer func() {
		_ = client.UpdateNodeStatus(nodeA, "offline")
		_ = client.UpdateNodeStatus(nodeB, "offline")
	}()

	// Create 6 live unassigned channels
	var chans []database.ChannelAssignment
	for i := 0; i < 6; i++ {
		chans = append(chans, database.ChannelAssignment{
			Username:   fmt.Sprintf("%s-ch%d", testNodeID, i),
			Site:       "chaturbate",
			Status:     "unassigned",
			IsLive:     true,
			Framerate:  60,
			Resolution: 1080,
		})
	}
	if err := client.BulkInsertAssignments(chans); err != nil {
		t.Fatalf("BulkInsertAssignments error: %v", err)
	}
	defer func() {
		for _, a := range chans {
			_ = client.DeleteAssignment(a.Username, a.Site)
		}
	}()

	// Simulate fair-share claim by each node
	// With 2 alive nodes and 6 live channels, fair share = ceil(6/2) = 3 each
	for _, id := range []string{nodeA, nodeB} {
		stats, err := client.GetAssignmentStats()
		if err != nil {
			t.Fatalf("GetAssignmentStats error: %v", err)
		}
		fairShare := 0
		if stats.TotalAliveNodes > 0 {
			fairShare = (stats.TotalLiveChannels + stats.TotalAliveNodes - 1) / stats.TotalAliveNodes
		}
		claimed, err := client.ClaimChannels(id, fairShare)
		if err != nil {
			t.Fatalf("ClaimChannels for %s error: %v", id, err)
		}
		t.Logf("Node %s claimed %d channels (fair share = %d)", id, len(claimed), fairShare)
	}

	// Verify total claimed = 6 (no double-claiming)
	all, err := client.GetAllAssignments()
	if err != nil {
		t.Fatalf("GetAllAssignments error: %v", err)
	}
	totalClaimed := 0
	for _, a := range all {
		if a.AssignedNode == nodeA || a.AssignedNode == nodeB {
			// Only count our test channels
			for i := 0; i < 6; i++ {
				if a.Username == fmt.Sprintf("%s-ch%d", testNodeID, i) {
					totalClaimed++
					break
				}
			}
		}
	}
	if totalClaimed != 6 {
		t.Errorf("total claimed = %d, want 6 (all channels should be assigned)", totalClaimed)
	}

	// Verify no channel is assigned to both nodes
	claimedBy := map[string]string{}
	for _, a := range all {
		if a.AssignedNode == "" {
			continue
		}
		if prev, ok := claimedBy[a.Username]; ok && prev != a.AssignedNode {
			t.Errorf("split-brain: %s claimed by both %s and %s", a.Username, prev, a.AssignedNode)
		}
		claimedBy[a.Username] = a.AssignedNode
	}
}

// TestIntegrationNodeStatusTransitions tests online → draining → offline.
func TestIntegrationNodeStatusTransitions(t *testing.T) {
	client := skipIfNoSupabase(t)
	defer cleanupNode(t, client)

	coord := New(client, &mockChannelManager{})
	coord.NodeID = testNodeID
	coord.Mode = entity.PoolModePooled
	coord.Register()

	if err := client.UpdateNodeStatus(testNodeID, "draining"); err != nil {
		t.Fatalf("UpdateNodeStatus draining error: %v", err)
	}
	node, _ := client.GetNode(testNodeID)
	if node.Status != "draining" {
		t.Errorf("status = %q, want draining", node.Status)
	}

	if err := client.UpdateNodeStatus(testNodeID, "offline"); err != nil {
		t.Fatalf("UpdateNodeStatus offline error: %v", err)
	}
	node, _ = client.GetNode(testNodeID)
	if node.Status != "offline" {
		t.Errorf("status = %q, want offline", node.Status)
	}
}

// TestIntegrationReaperEligibility verifies that channels on dead nodes are reclaimable.
func TestIntegrationReaperEligibility(t *testing.T) {
	client := skipIfNoSupabase(t)
	defer cleanupAssignments(t, client, testNodeID)
	defer cleanupNode(t, client)

	// Register node
	_ = client.UpsertNode(&database.Node{
		NodeID: testNodeID,
		Hostname: "test-host",
		Status: "online",
		// Set heartbeat to far in the past
		LastHeartbeat: time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
	})

	// Assign a channel to this dead node
	assignment := database.ChannelAssignment{
		Username:     testNodeID + "-dead-ch",
		Site:         "chaturbate",
		AssignedNode: testNodeID,
		Status:       "claimed",
		IsLive:       true,
		Framerate:    60,
		Resolution:   1080,
	}
	if err := client.BulkInsertAssignments([]database.ChannelAssignment{assignment}); err != nil {
		t.Fatalf("BulkInsertAssignments error: %v", err)
	}
	defer func() {
		_ = client.DeleteAssignment(assignment.Username, assignment.Site)
	}()

	// Get dead nodes using 180s timeout (standard reaper timeout)
	deadIDs, err := client.GetDeadNodes(180 * time.Second)
	if err != nil {
		t.Fatalf("GetDeadNodes error: %v", err)
	}

	found := false
	for _, id := range deadIDs {
		if id == testNodeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("node %q should be in dead list (heartbeat > 180s old)", testNodeID)
	}

	// Reclaim channels from dead node
	reclaimed, err := client.ReclaimChannels(testNodeID)
	if err != nil {
		t.Fatalf("ReclaimChannels error: %v", err)
	}
	if reclaimed == 0 {
		t.Error("expected at least 1 channel to be reclaimed")
	}
}
