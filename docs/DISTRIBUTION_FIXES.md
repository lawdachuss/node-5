# Channel Distribution Fixes Plan

## Problem
1-2 nodes claim all active (live) channels to record, leaving other nodes idle or underutilized.

## Root Causes Identified (8 bugs)

| ID | Severity | Description | File |
|----|----------|-------------|------|
| B1 | CRITICAL | `claim_channels` RPC uses `ORDER BY username ASC` — earliest-starting node always gets alphabetically-first channels | Supabase RPC (not in repo) |
| B2 | CRITICAL | Offline shuffle aborts entirely when node has ANY active recording — busy nodes never redistribute offline channels | `coordinator/shuffle.go:156` |
| B3 | CRITICAL | Live channels count toward `myLoad` but never get released — blocks capacity for offline distribution | `coordinator/assignment.go:163` |
| B4 | HIGH | No rebalance at session boundaries — channels stay on same node forever across restarts | `manager/manager.go:828` |
| S1 | HIGH | PostgREST 1000-row limit silently truncates all `channel_assignments` queries — wrong counts | `database/supabase.go` (multiple) |
| S2 | HIGH | Deadline migration sends ALL channels to the SAME target node (by-value copy bug) | `coordinator/shuffle.go:328` |
| S5 | MEDIUM | Fence can't release channels to DB during partition (DB unreachable) — double-recording window | `coordinator/node.go:103` |
| S6 | MEDIUM | Retry loop has no total deadline — can hang for 194s, stalling all coordinator loops | `database/supabase.go:62` |

## Constraint
> "If once recording started then it must stay in that node until the new session starts"

This means live/recording channels are NEVER migrated mid-session. Redistribution only happens:
- When a channel goes offline naturally → release to pool → another node claims it
- At session boundaries (after uploads complete, before resume) → forced rebalance

## Fix Plan

### Fix 1 — Randomize claim order (B1)
**File:** `database/migrate-v3.sql` (new)
**Change:** Update `claim_channels` RPC from `ORDER BY username ASC` to `ORDER BY RANDOM()`
**Why:** Every node gets a random subset of channels instead of bias toward alphabetically-first channels

### Fix 2 — Fix shuffle skip (B2)
**File:** `coordinator/shuffle.go:156-158`
**Change:** Remove `if len(localSet) > 0 { return }` block
**Why:** A node with live recordings should still redistribute its OFFLINE channels to other nodes. The filter at lines 160-170 already protects live/recording channels from being moved.

### Fix 3 — Live-aware fair-share (B3)
**File:** `coordinator/assignment.go:152,163-201`
**Change:** Calculate effective capacity = `fairShare - myLiveCount`. Only claim/release offline channels within this capacity. Live channels fill a node's quota without blocking offline distribution.

### Fix 4 — Session-boundary rebalance (B4)
**File:** `manager/manager.go:826-829` and `coordinator/assignment.go`
**Change:** After upload processing completes and before `ResumeAllChannels`, trigger an immediate claim cycle. This releases excess offline channels and picks up new ones while no recordings are active.

### Fix 5 — PostgREST 1000-row limit (S1)
**File:** `database/supabase.go` — all channel_assignments queries
**Change:** Add `limit=5000000` and proper pagination to all queries. Use Range headers for queries that could exceed 1000 rows.

### Fix 6 — Deadline migration load-spreading (S2)
**File:** `coordinator/shuffle.go:318-330`
**Change:** Replace by-value `target.CurrentLoad++` with a local load map (like `runOfflineShuffleCycle` does at line 182-185)

### Fix 7 — Fence releases unconditionally (S5)
**File:** `coordinator/node.go:103-116`
**Change:** Stop local recording BEFORE attempting to release DB channels. This ensures the node stops recording even if DB is unreachable during partition.

### Fix 8 — Retry context deadlines (S6)
**File:** `database/supabase.go:62-125`
**Change:** Pass a context with total deadline (45s) to `requestWithRetry` so goroutines don't hang for 194s.

## Implementation Order
1. Fix 1 (RPC randomization) — lowest risk, biggest impact
2. Fix 2 (shuffle skip) — 3 lines deleted
3. Fix 3 (live-aware fair-share) — moderate changes
4. Fix 4 (session-boundary rebalance) — hooks into session loop
5. Fix 5 (PostgREST limit) — defensive, prevents silent truncation
6. Fix 6 (deadline migration) — 5 lines changed
7. Fix 7 (fence release) — reorder operations
8. Fix 8 (retry context) — adds context plumbing
