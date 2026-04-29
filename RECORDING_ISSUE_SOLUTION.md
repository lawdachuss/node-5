# Recording Issue - Complete Analysis & Solution

## Current Status

### ✅ What's Working
- 5 FlareSolverr instances running and healthy
- Docker containers all operational
- Web UI accessible at http://54.210.37.19:8080
- Configuration files properly loaded
- Debug mode enabled

### ❌ What's NOT Working
- **0 active recordings** despite 268 channels configured
- **All channels blocked by Cloudflare**
- **FlareSolverr is NOT being called** despite being configured

## Root Cause

After deep code analysis, I discovered:

1. **FlareSolverr fallback code EXISTS** in `chaturbate/chaturbate.go` (lines 101-175)
2. **Cloudflare blocks ARE detected** (logs confirm this)
3. **BUT the fallback is NOT triggered** - no debug logs, no FlareSolverr requests

**The Problem**: The FlareSolverr fallback code is not being reached, likely due to:
- Error wrapping preventing `errors.Is()` from matching `ErrCloudflareBlocked`
- OR the error path is different than expected
- OR cookies are stale and causing a different type of block

## Evidence

### From Logs
```
2026/04/29 19:41:34  INFO [stoneinthesword] channel was blocked by Cloudflare
2026/04/29 19:41:35  INFO [kylianna] channel was blocked by Cloudflare
... (all 268 channels showing same error)
```

### From FlareSolverr Logs
```
GET / (health checks only)
NO POST /v1 requests (no actual Cloudflare bypass attempts)
```

### From Code Analysis
The fallback should trigger here (`chaturbate/chaturbate.go:101`):
```go
if errors.Is(err, internal.ErrCloudflareBlocked) {
    // This code is NOT being executed!
    fmt.Printf("[DEBUG] Cloudflare block detected, trying FlareSolverr scraping...\n")
    // ... 5 retry attempts with FlareSolverr ...
}
```

## Solutions

### Option 1: Add Diagnostic Logging (IMMEDIATE)
Add detailed logging to see WHY the fallback isn't triggering:

**File**: `chaturbate/chaturbate.go` around line 100
```go
if err != nil {
    // ADD THIS:
    fmt.Printf("[DEBUG] FetchStream error: %v (type: %T)\n", err, err)
    fmt.Printf("[DEBUG] Is ErrCloudflareBlocked? %v\n", errors.Is(err, internal.ErrCloudflareBlocked))
    
    // Existing code:
    if errors.Is(err, internal.ErrCloudflareBlocked) {
        ...
    }
}
```

### Option 2: Force FlareSolverr Usage (RECOMMENDED FIX)
Instead of using FlareSolverr as a fallback, make it the PRIMARY method:

**File**: `chaturbate/chaturbate.go` around line 95
```go
func FetchStream(ctx context.Context, client *internal.Req, username string) (*Stream, error) {
    // TRY FLARESOLVERR FIRST instead of POST API
    hlsURL, status, err := internal.ScrapeChaturbateStreamWithFlareSolverr(ctx, username)
    if err == nil && hlsURL != "" {
        return &Stream{HLSSource: hlsURL}, nil
    }
    
    // Fall back to POST API if FlareSolverr fails
    body, err := internal.PostChaturbateAPI(ctx, username, csrfToken)
    ...
}
```

### Option 3: Fix Error Wrapping
Ensure errors are properly wrapped to maintain `errors.Is()` compatibility:

**File**: `internal/chaturbate_req.go`
```go
// WRONG (breaks errors.Is):
return "", fmt.Errorf("request failed: %v", ErrCloudflareBlocked)

// RIGHT (preserves errors.Is):
return "", fmt.Errorf("request failed: %w", ErrCloudflareBlocked)
```

### Option 4: Get Fresh Cookies
The cookies might be stale. Use FlareSolverr to get fresh ones:

```bash
curl -X POST http://54.210.37.19:8191/v1 \
  -H "Content-Type: application/json" \
  -d '{
    "cmd": "request.get",
    "url": "https://chaturbate.com/",
    "maxTimeout": 60000
  }'
```

Then update `conf/settings.json` with the new cookies.

## Recommended Action Plan

### Phase 1: Diagnosis (5 minutes)
1. Add diagnostic logging (Option 1)
2. Rebuild and deploy
3. Check logs to see actual error type

### Phase 2: Fix (15 minutes)
1. Implement Option 2 (Force FlareSolverr) OR fix error wrapping (Option 3)
2. Rebuild Docker image
3. Deploy to EC2
4. Monitor logs for FlareSolverr activity

### Phase 3: Verify (5 minutes)
1. Check FlareSolverr logs for POST /v1 requests
2. Verify recordings are starting
3. Check videos directory for files

## Quick Test Commands

### Check if FlareSolverr is accessible from container:
```bash
docker exec goondvr curl -X POST http://flaresolverr-1:8191/v1 \
  -H "Content-Type: application/json" \
  -d '{"cmd":"request.get","url":"https://chaturbate.com/","maxTimeout":60000}'
```

### Monitor FlareSolverr logs:
```bash
docker logs flaresolverr-1 --follow
```

### Monitor recorder logs:
```bash
docker logs goondvr --follow | grep -i "debug\|flare"
```

## Files That Need Changes

1. **chaturbate/chaturbate.go** - Add logging or change to use FlareSolverr first
2. **internal/chaturbate_req.go** - Fix error wrapping if needed
3. **Dockerfile** - Rebuild after code changes

## Next Steps

Would you like me to:
1. **Add diagnostic logging** to see why fallback isn't triggering?
2. **Implement the fix** to force FlareSolverr usage?
3. **Test FlareSolverr manually** to verify it works?
4. **Get fresh cookies** from FlareSolverr?

Let me know which approach you prefer, and I'll implement it!
