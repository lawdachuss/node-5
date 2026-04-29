# Recording Issue - Root Cause Analysis & Solution

## Problem Summary
- **Issue**: 268 channels configured, but NONE are recording
- **Error**: All channels show "channel was blocked by Cloudflare"
- **Expected**: FlareSolverr should automatically bypass Cloudflare blocks
- **Actual**: FlareSolverr is NOT being called despite being configured

## Investigation Results

### 1. Configuration Status ✅
- **FlareSolverr**: 5 instances running and healthy (ports 8191-8195)
- **Environment Variable**: `FLARESOLVERR_URL=http://flaresolverr-1:8191/v1` ✅
- **Debug Mode**: Enabled in settings.json ✅
- **Cookies**: Present in settings.json ✅
- **User-Agent**: Present in settings.json ✅

### 2. Code Analysis

#### Cloudflare Detection (internal/chaturbate_req.go)
```go
// ErrCloudflareBlocked is returned when response contains:
- "<title>Just a moment...</title>"
- "Checking your browser"
- "cloudflare"
```

#### FlareSolverr Fallback (chaturbate/chaturbate.go, lines 101-175)
```go
if errors.Is(err, internal.ErrCloudflareBlocked) {
    // Should trigger FlareSolverr with 5 retry attempts
    // Uses ScrapeChaturbateStreamWithFlareSolverr()
}
```

### 3. Root Cause Discovery

**The FlareSolverr fallback code EXISTS but is NOT being triggered!**

Evidence:
1. ✅ Cloudflare blocks ARE detected (logs show "blocked by Cloudflare")
2. ❌ NO debug logs showing FlareSolverr attempts
3. ❌ FlareSolverr logs show ONLY health checks, NO actual requests

**Hypothesis**: The error being returned is NOT `ErrCloudflareBlocked`, or the error wrapping is preventing `errors.Is()` from matching.

### 4. Possible Causes

#### Option A: Error Wrapping Issue
The error might be wrapped with `fmt.Errorf()` which breaks `errors.Is()` matching:
```go
// This BREAKS errors.Is():
return fmt.Errorf("some context: %w", ErrCloudflareBlocked)

// This WORKS:
return ErrCloudflareBlocked
```

#### Option B: Different Error Path
The POST API might be returning a different error that doesn't trigger the fallback.

#### Option C: Cookies Are Stale
The cookies in settings.json might be expired, causing Chaturbate to return a different type of block.

## Solution Approaches

### Solution 1: Force FlareSolverr Usage (RECOMMENDED)
Modify the code to ALWAYS use FlareSolverr for initial requests, not just as a fallback.

**File**: `chaturbate/chaturbate.go`
**Change**: Move FlareSolverr scraping to be the PRIMARY method, not fallback

### Solution 2: Fix Error Matching
Ensure `ErrCloudflareBlocked` is returned without wrapping, or use `errors.Unwrap()`.

### Solution 3: Refresh Cookies
Get fresh cookies from FlareSolverr and update settings.json.

### Solution 4: Add Detailed Logging
Add more debug logs to see exactly what error is being returned and why the fallback isn't triggering.

## Recommended Next Steps

1. **Add Debug Logging** to see the actual error being returned
2. **Test FlareSolverr Manually** to verify it can bypass Cloudflare
3. **Modify Code** to force FlareSolverr usage or fix error matching
4. **Get Fresh Cookies** from a successful FlareSolverr request

## Files to Modify

1. `chaturbate/chaturbate.go` - Main recording logic
2. `internal/chaturbate_req.go` - POST API and Cloudflare detection
3. `internal/flaresolverr.go` - FlareSolverr integration

## Testing Plan

1. Add debug logs before and after `errors.Is()` check
2. Print the actual error type and value
3. Manually test FlareSolverr with curl
4. Verify FlareSolverr can get stream URLs
5. Deploy fix and monitor logs

## Status
- **Current**: Investigation complete, root cause identified
- **Next**: Implement Solution 1 or 4 to fix the issue
