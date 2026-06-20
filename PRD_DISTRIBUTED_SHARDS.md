# Distributed Shards/Nodes Architecture — Product Requirements Document

**Version**: 1.0  
**Last Updated**: 2026-06-20  
**Status**: Plan — not yet implemented

---

## Table of Contents

1. [Problem Statement](#1-problem-statement)
2. [Glossary](#2-glossary)
3. [Architecture Overview](#3-architecture-overview)
4. [Database Schema Changes](#4-database-schema-changes)
5. [Auto Node ID Detection](#5-auto-node-id-detection)
6. [New Package: `coordinator/`](#6-new-package-coordinator)
7. [Modified Channel Lifecycle](#7-modified-channel-lifecycle)
8. [Session Handling](#8-session-handling)
9. [Web UI Changes](#9-web-ui-changes)
10. [Deployment & Multi-Repo CI/CD](#10-deployment--multi-repo-cicd)
11. [Edge Cases & Failure Modes](#11-edge-cases--failure-modes)
12. [Migration Strategy](#12-migration-strategy)
13. [Testing Strategy](#13-testing-strategy)
14. [Rollback Plan](#14-rollback-plan)
15. [Implementation Phases](#15-implementation-phases)

---

## 1. Problem Statement

**Current state**: One GitHub repo, one RDP, one Go binary records ALL configured channels. If 20 channels are live, all 20 run on a single Windows machine with a single IP.

**Goals**:
- Split channels dynamically across N independent nodes (each on its own RDP/repo)
- All nodes share a single Supabase database
- Automatic load balancing: if 20 channels and 5 nodes → 4 each
- Zero manual intervention after setup
- No lost recordings during node failures or reassignments

---

## 2. Glossary

| Term | Definition |
|---|---|
| **Node** | A running Go binary instance on one RDP. Unique `NODE_ID`. Owns a subset of channels. |
| **Channel Pool** | Shared JSON blob in `app_settings` (key `channel_pool`) — ALL channels to record. |
| **Channel Assignment** | Row in `channel_assignments` table linking a channel to a node. |
| **Shard** | The subset of channels assigned to a particular node at a given time. |
| **Fair Share** | `ceil(total_live_channels / total_alive_nodes)` — target count per node. |
| **Orphan** | A channel whose assigned node has stopped heartbeating. |
| **Heartbeat** | Periodic `last_heartbeat` update in the `nodes` table (every 30s). |
| **Live** | A channel currently streaming on Chaturbate/Stripchat (has active HLS). |

---

## 3. Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                       Supabase (PostgreSQL)                   │
│  ┌──────────────┐  ┌──────────────────┐  ┌────────────────┐ │
│  │ nodes        │  │ channel_assign   │  │ app_settings   │ │
│  │ (heartbeat)  │  │ ments            │  │ - channel_pool │ │
│  │              │  │ (who owns what)  │  │ - dvr_settings │ │
│  └──────────────┘  └──────────────────┘  └────────────────┘ │
│  ┌──────────────┐  ┌──────────────────┐  ┌────────────────┐ │
│  │ recordings   │  │ upload_journal   │  │ pipeline_      │ │
│  │ (shared)     │  │ (dedup all nodes)│  │ states         │ │
│  └──────────────┘  └──────────────────┘  └────────────────┘ │
└──────────────────────────────┬───────────────────────────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
┌───────▼───────┐    ┌────────▼───────┐    ┌────────▼───────┐
│ Node "node-a" │    │ Node "node-b"  │    │ Node "node-c"  │
│ (RDP 1)       │    │ (RDP 2)        │    │ (RDP 3)        │
│ Repo: ...-a   │    │ Repo: ...-b    │    │ Repo: ...-c    │
│               │    │                │    │                │
│ Channels:     │    │ Channels:      │    │ Channels:      │
│  alice        │    │  bob           │    │  carol         │
│  dave         │    │  eve           │    │  frank         │
│  grace        │    │  henry         │    │  iris          │
└───────────────┘    └────────────────┘    └────────────────┘
```

### Mode: Backward Compatible

| Mode | Env Var | Behavior |
|---|---|---|
| **Isolated** (default) | unset or `CHANNEL_POOL_MODE=isolated` | Existing behavior — each node has own `channels_<INSTANCE_ID>` |
| **Pooled** (new) | `CHANNEL_POOL_MODE=pooled` | Shared pool, dynamic fair-share assignment |

---

## 4. Database Schema Changes

### 4.1 New Table: `nodes`

```sql
CREATE TABLE IF NOT EXISTS nodes (
    node_id          TEXT PRIMARY KEY,
    hostname         TEXT NOT NULL DEFAULT '',
    instance_label   TEXT NOT NULL DEFAULT '',
    software_version TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'offline'
                     CHECK (status IN ('online','offline','draining')),
    current_load     INT NOT NULL DEFAULT 0,
    last_heartbeat   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
CREATE INDEX IF NOT EXISTS idx_nodes_heartbeat ON nodes(last_heartbeat);
```

### 4.2 New Table: `channel_assignments`

```sql
CREATE TABLE IF NOT EXISTS channel_assignments (
    username        TEXT NOT NULL,
    site            TEXT NOT NULL DEFAULT 'chaturbate'
                    CHECK (site IN ('chaturbate','stripchat')),
    assigned_node   TEXT REFERENCES nodes(node_id),
    status          TEXT NOT NULL DEFAULT 'unassigned'
                    CHECK (status IN ('unassigned','claimed','recording','paused','error')),
    is_live         BOOLEAN NOT NULL DEFAULT FALSE,
    live_checked_at TIMESTAMPTZ,
    assigned_at     TIMESTAMPTZ,
    last_heartbeat  TIMESTAMPTZ,
    -- Config snapshot (duplicated from pool for atomic self-contained claims)
    framerate       INT NOT NULL DEFAULT 60,
    resolution      INT NOT NULL DEFAULT 2160,
    pattern         TEXT NOT NULL DEFAULT '',
    max_duration    INT NOT NULL DEFAULT 60,
    max_filesize    INT NOT NULL DEFAULT 0,
    compress        BOOLEAN NOT NULL DEFAULT FALSE,
    min_duration_before_upload INT NOT NULL DEFAULT 1200,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (username, site)
);
CREATE INDEX IF NOT EXISTS idx_ca_assigned_node ON channel_assignments(assigned_node);
CREATE INDEX IF NOT EXISTS idx_ca_status ON channel_assignments(status);
CREATE INDEX IF NOT EXISTS idx_ca_islive ON channel_assignments(is_live);
CREATE INDEX IF NOT EXISTS idx_ca_heartbeat ON channel_assignments(last_heartbeat);
```

### 4.3 New Pool Key in `app_settings`

Key: `channel_pool`  
Value: JSON array of `ChannelConfig` (same structure as existing `channels_<INSTANCE_ID>`)

### 4.4 Modified: `pipeline_states`

```sql
ALTER TABLE pipeline_states ADD COLUMN IF NOT EXISTS node_id TEXT;
```

---

## 5. Auto Node ID Detection

Priority chain in `server/db.go`:

```go
func detectNodeID() string {
    if id := os.Getenv("NODE_ID"); id != "" { return id }
    if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" {
        parts := strings.Split(repo, "-")
        if len(parts) > 1 { return parts[len(parts)-1] }
        return strings.ReplaceAll(repo, "/", "-")
    }
    if host, err := os.Hostname(); err == nil && host != "" { return host }
    return fmt.Sprintf("node-%x", rand.Uint64())
}
```

---

## 6. New Package: `coordinator/`

### 6.1 File Structure

```
coordinator/
├── coordinator.go       # Coordinator struct, lifecycle, mode detection
├── node.go              # NodeRegistry — register, heartbeat, deregister
├── assignment.go        # AssignmentManager — claim, release, fair-share calc
├── liveness.go          # LiveChecker — periodic channel liveness poll
├── reaper.go            # OrphanReaper — detect dead nodes, reclaim channels
└── coordinator_test.go  # Unit tests
```

### 6.2 `coordinator.go`

```go
type Coordinator struct {
    NodeID   string
    Mode     string
    Client   *database.Client
    manager  *manager.Manager
    stopCh   chan struct{}
    wg       sync.WaitGroup
}

func New(client *database.Client, mgr *manager.Manager) *Coordinator
func (c *Coordinator) Start(ctx context.Context)
func (c *Coordinator) Stop()
func (c *Coordinator) IsPooled() bool
```

### 6.3 `node.go`

| Method | Description | Interval |
|---|---|---|
| `Register()` | Upsert node row with hostname, version | Once on startup |
| `HeartbeatLoop(ctx)` | Update `last_heartbeat` every 30s | Every 30s |
| `Deregister()` | Set status = 'offline' | On shutdown |

### 6.4 `assignment.go` — Core Fair-Share Algorithm

```
1. Query: totalLiveChannels, totalAliveNodes
2. fairShare = ceil(totalLive / totalAlive)
3. myLoad = count of my assignments
4. budget = fairShare - myLoad
5. if budget > 0: ClaimChannels(nodeID, budget)
6. For each claimed: manager.CreateChannelFromAssignment(ca)
```

**Atomic Claim**: `PATCH /channel_assignments?assigned_node=is.null&status=eq.unassigned&is_live=eq.true&limit={budget}` with `Prefer: return=representation`

### 6.5 `liveness.go`

Every 120s (+ jitter): check ALL pool channels for live status, bulk-update `is_live` in `channel_assignments`.

### 6.6 `reaper.go`

Every 120s: find nodes with `last_heartbeat < NOW() - 180s`, reclaim their channels.

---

## 7. Modified Channel Lifecycle

### 7.1 Startup Flow (pooled mode)

```
1. Parse CLI flags
2. Load settings (cookies) from Supabase
3. Create Manager
4. Create Coordinator (reads CHANNEL_POOL_MODE, detects NODE_ID)
5. Start signal handler (extended)
6. IF pooled:
     pool = LoadPoolFromDB()
     myAssignments = GetNodeAssignments(nodeID)
     for each pool channel in myAssignments: CreateChannel()
   ELSE:
     Manager.LoadConfig()  // existing behavior
7. Coordinator.Start()    // register, heartbeat, claim, live check, reaper
8. Manager.StartSession()
9. Start HTTP server
```

### 7.2 Channel Creation (pooled mode)

```
1. Add to channel_pool blob
2. Insert in channel_assignments (status='unassigned')
3. ClaimSpecificChannel(username, site, nodeID)
4. If claimed: CreateChannel(), start monitor
5. Save pool to Supabase
```

### 7.3 Channel Deletion (pooled mode)

```
1. Remove from memory
2. ReleaseAssignment(username, site)
3. Remove from channel_pool blob
4. Save pool to Supabase
5. Stop channel async
```

### 7.4 Graceful Shutdown (pooled mode)

```
1. Coordinator.StartDraining() → status='draining'
2. StopAllChannels()
3. WaitForAllChannels()
4. WaitForUploads()
5. Coordinator.ReleaseAllChannels()  ← after uploads done
6. Coordinator.Deregister() → status='offline'
```

---

## 8. Session Handling (Per-Node)

- Each node has its own `SESSION_DURATION`
- Session ends → `StopWithProcessingQueue(10)` → pipelines complete → channels stay **assigned**
- Next session within 180s resumes those channels
- Node never comes back → orphan reaper after 180s
- **No recordings lost**: pipelines finish, files upload before release

---

## 9. Web UI Changes

- Global dashboard: all channels from pool with `assigned_node` badge
- Nodes dashboard: alive nodes, status, load, heartbeat freshness
- SSE still broadcasts this node's own channels
- UI polls Supabase REST every 30s for global state

---

## 10. Deployment & Multi-Repo CI/CD

**Template repo**: `MiniDelectableService` — main development, `sync-nodes.yml` pushes to node repos
**Node repos**: `MiniDelectableService-node-a`, `node-b`, `node-c` — each has own secure-rdp workflow

Sync workflow:
```yaml
on: push branches:[main]
strategy.matrix.node: [node-a, node-b, node-c]
steps: git push to each node repo --force
```

---

## 11. Edge Cases & Failure Modes

### Key Mitigations

| Risk | Mitigation |
|---|---|
| Cold start race | Jitter + atomic PATCH (row-level Supabase locking) |
| Node crash | 180s heartbeat timeout → orphan reaper |
| Split-brain | Ownership verification every 60s during recording + file-hash dedup |
| Node clock skew | Server-side `NOW()` for comparisons |
| Config mid-recording | Next claim cycle refreshes from pool |
| Code version mismatch | JSON backward compat (extra fields ignored) |
| Supabase outage | Heartbeats fail → all nodes "dead" → on recovery, first heartbeat revives → reclaim channels |

---

## 12. Migration Strategy

1. Run `database/migrate-v2.sql` in Supabase
2. Merge existing `channels_<INSTANCE_ID>` blobs into `channel_pool`
3. Bulk-insert `channel_assignments` with `status='unassigned'`
4. Set `CHANNEL_POOL_MODE=pooled` on all nodes
5. Restart all nodes
6. Verify distribution

**Rollback**: Set `CHANNEL_POOL_MODE=isolated` → instant revert, no data loss.

---

## 13. Testing Strategy

| Type | Tests |
|---|---|
| **Unit** | `TestDetectNodeID`, `TestFairShareCalculation`, `TestClaimChannelsAtomic`, `TestOrphanReaper`, `TestOwnershipVerification` |
| **Integration** | Two nodes same pool → channels divided without overlap. Node crash + recovery. Node join mid-operation. |
| **E2E** | 3-node RDP deployment. Kill one node → redistribute. 24h stability. |

---

## 14. Rollback Plan

| Scenario | Action | Data Loss |
|---|---|---|
| Bug in claim logic | `CHANNEL_POOL_MODE=isolated`, restart | None |
| Orphan reaper false positive | Stop one node | Max duplicate segments |
| Migration corruption | Restore Supabase backup | Minutes of recordings |
| Complete failure | Keep `isolated` mode (default) | None — system unchanged |

---

## 15. Implementation Phases

### Phase 1 ✅ — Database Schema + REST Methods
- [x] Create `database/migrate-v2.sql`
- [ ] Add `Node` and `ChannelAssignment` structs in `entity/entity.go`
- [ ] Add `node_id` field to `PipelineState` in `entity/entity.go`
- [ ] Add REST methods in `database/supabase.go`:
  - `UpsertNode`, `HeartbeatNode`, `UpdateNodeStatus`, `GetDeadNodes`
  - `ClaimChannels`, `ClaimSpecificChannel`, `ReleaseNodeChannels`, `ReleaseChannel`
  - `GetNodeAssignments`, `GetAssignment`, `GetAssignmentStats`
  - `SetChannelsLive`, `GetChannelsLiveStatus`
  - `LoadPoolFromDB`, `SavePoolToDB`
  - `GetAllAppSettingKeys`
- [ ] Add pool key helpers in `server/db.go`
- [ ] Add `detectNodeID()` in `server/db.go`

### Phase 2 — Coordinator Package
- [ ] Create `coordinator/coordinator.go`
- [ ] Create `coordinator/node.go`
- [ ] Create `coordinator/assignment.go`
- [ ] Create `coordinator/liveness.go`
- [ ] Create `coordinator/reaper.go`
- [ ] Create `coordinator/coordinator_test.go`

### Phase 3 — Manager Integration
- [ ] Add `CHANNEL_POOL_MODE` env var
- [ ] Add `LoadPooledConfig()` in `manager/manager.go`
- [ ] Modify `CreateChannel` for pooled mode
- [ ] Modify `StopChannel` for pooled mode
- [ ] Modify shutdown flow to include Coordinator
- [ ] Add `CreateChannelFromAssignment()` helper
- [ ] Add ownership verification in `channel/channel_record.go`

### Phase 4 — main.go Wiring
- [ ] Add Coordinator initialization in `main.go`
- [ ] Add pooled mode startup branch
- [ ] Extend signal handler for Coordinator shutdown
- [ ] Add `--channel-pool-mode` CLI flag

### Phase 5 — Web UI + Templates
- [ ] Add global channel dashboard
- [ ] Add nodes dashboard
- [ ] Add pool config editor
- [ ] Add 30s Supabase polling for global state
- [ ] Modify SSE for pooled-aware updates

### Phase 6 — CI/CD + Deployment
- [ ] Create template repo sync workflow
- [ ] Create node repo setup script
- [ ] Add migration script
- [ ] Update `.env.example`
- [ ] Write deployment docs in README

### Phase 7 — Testing + Hardening
- [ ] Unit tests for all coordinator methods
- [ ] Integration tests with test Supabase project
- [ ] Split-brain test harness
- [ ] 24h stability test
- [ ] Load test: 100 channels, 5 nodes

---

## Appendix A: Env Var Reference

| Variable | Default | Description |
|---|---|---|
| `NODE_ID` | Auto-detected | Unique node identifier |
| `INSTANCE_LABEL` | `""` | Human-readable label for node |
| `CHANNEL_POOL_MODE` | `isolated` | `isolated` or `pooled` |
| `INSTANCE_ID` | `default` | Used for isolated mode backward compat |

## Appendix B: CLI Flags Reference

| Flag | Env Var | Description |
|---|---|---|
| `--channel-pool-mode` | `CHANNEL_POOL_MODE` | Channel distribution mode |
| `--node-id` | `NODE_ID` | Node identifier (auto if unset) |

## Appendix C: File Change Summary

**New files** (~740 lines):
- `coordinator/coordinator.go`, `coordinator/node.go`, `coordinator/assignment.go`
- `coordinator/liveness.go`, `coordinator/reaper.go`, `coordinator/coordinator_test.go`
- `database/migrate-v2.sql`
- `.github/workflows/sync-nodes.yml`

**Modified files** (~910 lines):
- `entity/entity.go`, `database/supabase.go`, `server/db.go`, `server/config.go`
- `manager/manager.go`, `main.go`, `router/router_handler.go`
- `router/view/templates/*.html`, `channel/channel_record.go`
- `.env.example`, `README.md`

**Total**: ~1650 lines new code + SQL
