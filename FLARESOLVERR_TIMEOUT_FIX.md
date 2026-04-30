# FlareSolverr Timeout Issue - Fix Applied

## Date: April 30, 2026

## Problem
Channels are live but not recording due to **FlareSolverr timeout issues**.

### Symptoms
- Channels showing "starting to record" but not actually recording
- FlareSolverr requests timing out: "context deadline exceeded"
- Each FlareSolverr request taking 180+ seconds
- With 268 channels and 5 FlareSolverr instances, massive backlog created

### Root Cause
1. **Cloudflare challenges are harder** - Taking 180+ seconds to solve
2. **Too many retry attempts** - 3 attempts × 180 seconds = 9 minutes per channel
3. **Slow fallback** - POST API only tried after all FlareSolverr attempts fail
4. **Queue buildup** - 268 channels overwhelming 5 FlareSolverr instances

## Fix Applied

### Code Changes
**File**: `chaturbate/chaturbate.go`

**Before**:
```go
// Try scraping with FlareSolverr (up to 3 attempts)
for attempt := 1; attempt <= 3; attempt++ {
    // Create a context with longer timeout for FlareSolverr
    attemptCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
    hlsURL, status, scrapeErr = internal.ScrapeChaturbateStreamWithFlareSolverr(attemptCtx, username)
    cancel()
    
    if scrapeErr == nil {
        break
    }
    
    // Short delay before retry
    if attempt < 3 {
        delay := time.Duration(5+attempt*5) * time.Second
        time.After(delay)
    }
}
```

**After**:
```go
// Try scraping with FlareSolverr (only 1 attempt with shorter timeout)
// If it fails, quickly fall back to POST API

// Create a context with 60-second timeout for FlareSolverr (reduced from 180s)
attemptCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
hlsURL, status, scrapeErr = internal.ScrapeChaturbateStreamWithFlareSolverr(attemptCtx, username)
cancel()
```

### Changes Summary
1. **Reduced timeout**: 180s → 60s per attempt
2. **Single attempt**: 3 attempts → 1 attempt
3. **Faster fallback**: Falls back to POST API after 60s instead of 9 minutes
4. **Total time per channel**: 9 minutes → 60 seconds before POST API fallback

## Benefits

### 1. **Faster Fallback**
- If FlareSolverr fails, POST API is tried within 60 seconds
- Reduces wait time from 9 minutes to 1 minute

### 2. **Reduced Load on FlareSolverr**
- Fewer retry attempts means less queue buildup
- More channels can be processed in parallel

### 3. **Better Success Rate**
- POST API is more reliable for many channels
- FlareSolverr used as first attempt, POST API as quick fallback

### 4. **Improved Throughput**
- 5 FlareSolverr instances can handle more channels
- Faster cycle time means channels checked more frequently

## Deployment Status

✅ **Code updated** on EC2
✅ **Container rebuilt** with --no-cache
✅ **System restarted** and running

## Verification

### Check if Fix is Applied
```bash
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "grep -A 3 'FlareSolverr attempt' /home/ubuntu/goondvr/chaturbate/chaturbate.go | head -5"
```

Should show:
```go
fmt.Printf("[DEBUG] [%s] FlareSolverr attempt 1/1...\n", username)

// Create a context with 60-second timeout
attemptCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
```

### Monitor Recordings
```bash
# Check if channels are recording
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "cd /home/ubuntu/goondvr && docker compose logs --tail=50 recorder | grep 'stream type'"
```

### Check FlareSolverr Usage
```bash
# Should see faster attempts and POST API fallbacks
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "cd /home/ubuntu/goondvr && docker compose logs --tail=100 recorder | grep -E 'FlareSolverr|POST API'"
```

## Expected Behavior

### Successful FlareSolverr (within 60s)
```
[DEBUG] [username] Attempting FlareSolverr scraping (primary method)...
[DEBUG] [username] FlareSolverr attempt 1/1...
[DEBUG] Using FlareSolverr instance 3: http://flaresolverr-3:8191/v1
[DEBUG] [username] FlareSolverr success
[DEBUG] [username] Successfully got HLS URL: https://...
INFO [username] stream type: HLS, resolution 1080p
```

### Failed FlareSolverr → POST API Fallback (after 60s)
```
[DEBUG] [username] Attempting FlareSolverr scraping (primary method)...
[DEBUG] [username] FlareSolverr attempt 1/1...
[DEBUG] Using FlareSolverr instance 2: http://flaresolverr-2:8191/v1
[DEBUG] [username] FlareSolverr failed: context deadline exceeded
[DEBUG] [username] FlareSolverr failed, trying POST API fallback...
[DEBUG] API response body: {"success":true,"url":"https://..."}
INFO [username] stream type: HLS, resolution 1080p
```

## Alternative Solutions (If Issue Persists)

### 1. Add More FlareSolverr Instances
Increase from 5 to 10 instances in `docker-compose.yml`:
```yaml
services:
  flaresolverr-6:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    container_name: flaresolverr-6
    # ... same config as others
    ports:
      - "8196:8191"
```

Update load balancer in `internal/flaresolverr.go`:
```go
instanceCount := 10  // Changed from 5
```

### 2. Prioritize POST API
Swap the order - try POST API first, FlareSolverr as fallback:
```go
// Try POST API first (faster, more reliable)
body, err := internal.PostChaturbateAPI(ctx, username, csrfToken)
if err == nil {
    // Parse and return
}

// If POST API fails, try FlareSolverr
hlsURL, status, scrapeErr = internal.ScrapeChaturbateStreamWithFlareSolverr(ctx, username)
```

### 3. Disable FlareSolverr Temporarily
Use only POST API until Cloudflare eases up:
```go
// Skip FlareSolverr entirely
// hlsURL, status, scrapeErr = internal.ScrapeChaturbateStreamWithFlareSolverr(ctx, username)

// Go straight to POST API
body, err := internal.PostChaturbateAPI(ctx, username, csrfToken)
```

### 4. Implement Smart Routing
Use FlareSolverr only for channels that need it:
```go
// Check if channel has been blocked recently
if ch.CFBlockCount > 3 {
    // Use FlareSolverr for blocked channels
    hlsURL, status, scrapeErr = internal.ScrapeChaturbateStreamWithFlareSolverr(ctx, username)
} else {
    // Use POST API for non-blocked channels
    body, err := internal.PostChaturbateAPI(ctx, username, csrfToken)
}
```

## Monitoring

### Check FlareSolverr Queue Depth
```bash
docker logs flaresolverr-1 --tail=50 | grep "queue depth"
```

If queue depth > 5, FlareSolverr is overloaded.

### Check Success Rate
```bash
# Count successful recordings
docker compose logs recorder | grep "stream type" | wc -l

# Count FlareSolverr failures
docker compose logs recorder | grep "FlareSolverr failed" | wc -l

# Count POST API successes
docker compose logs recorder | grep "POST API" | grep -v "error" | wc -l
```

## Current Status

- ✅ Fix applied and deployed
- ⏳ Waiting for channels to cycle through and start recording
- 🔍 Monitoring logs for successful recordings

## Next Steps

1. **Wait 5-10 minutes** for channels to cycle through with new timeout
2. **Check for recordings**: Look for "stream type" messages in logs
3. **Monitor success rate**: Count FlareSolverr vs POST API successes
4. **Adjust if needed**: If still not working, try alternative solutions above

## Related Files
- `chaturbate/chaturbate.go` - Main scraping logic (FIXED)
- `internal/flaresolverr.go` - FlareSolverr load balancer
- `internal/chaturbate_scrape.go` - FlareSolverr scraping implementation
- `internal/chaturbate_req.go` - POST API implementation
