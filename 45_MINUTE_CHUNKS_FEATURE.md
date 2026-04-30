# 45-Minute Recording Chunks Feature

## Date: April 30, 2026

## Overview
Configured the recording system to automatically split streams into **45-minute chunks** continuously. This means each recording will be split into separate files every 45 minutes while the stream is live.

## Implementation

### How It Works
The system already had built-in support for splitting recordings by duration. The feature uses:
- `max_duration` parameter (in minutes) per channel
- Automatic file switching when duration threshold is reached
- Continuous recording across file boundaries (no gaps)

### Configuration Applied
Updated **all 268 channels** on EC2 to have:
```json
{
  "max_duration": 45,
  "max_filesize": 0
}
```

### File Naming
When recordings are split, files are automatically numbered with a sequence:
```
username_2026-04-30_10-00-00.mp4      (first 45 minutes)
username_2026-04-30_10-00-00_1.mp4    (next 45 minutes)
username_2026-04-30_10-00-00_2.mp4    (next 45 minutes)
... and so on
```

The `{{if .Sequence}}_{{.Sequence}}{{end}}` pattern in the filename template handles this automatically.

## Benefits

### 1. **Easier Upload Management**
- Smaller files (45 minutes ≈ 1-2GB) upload faster
- Less likely to hit upload size limits
- Failed uploads affect smaller chunks

### 2. **Better Storage Management**
- Can delete older chunks while keeping recent ones
- Easier to manage disk space
- More granular control over what to keep

### 3. **Improved Reliability**
- If recording crashes, only lose max 45 minutes
- Easier to resume from specific time points
- Less data loss on system failures

### 4. **Faster Processing**
- Smaller files process faster (ffmpeg remux, uploads)
- Can start uploading first chunk while still recording
- Parallel processing of multiple chunks

## Technical Details

### Code Location
The splitting logic is in `channel/channel_file.go`:

```go
func (ch *Channel) shouldSwitchFileLocked() bool {
    maxFilesizeBytes := int64(ch.Config.MaxFilesize) * 1024 * 1024
    maxDurationSeconds := ch.Config.MaxDuration * 60

    return (ch.Duration >= float64(maxDurationSeconds) && ch.Config.MaxDuration > 0) ||
        (ch.Filesize >= maxFilesizeBytes && ch.Config.MaxFilesize > 0)
}
```

This function is called in `handleSegmentForMonitor()` which checks after each segment if it's time to switch files.

### Seamless Switching
When the 45-minute threshold is reached:
1. Current file is finalized and closed
2. New file is created with incremented sequence number
3. Recording continues without interruption
4. No frames are lost during the switch

### Configuration File
Location: `/home/ubuntu/goondvr/conf/channels.json` on EC2

Each channel has:
```json
{
  "username": "channel_name",
  "max_duration": 45,    // Minutes
  "max_filesize": 0,     // MB (0 = disabled)
  ...
}
```

## Verification

### Check Current Settings
```bash
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "sudo cat /home/ubuntu/goondvr/conf/channels.json | jq '.[0] | {username, max_duration}'"
```

### Monitor File Switching
Watch the logs for messages like:
```
INFO [username] max filesize or duration exceeded, new file created: videos/username_2026-04-30_10-00-00_1.mp4
```

### Check Recording Files
```bash
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "ls -lh /home/ubuntu/goondvr/videos/*.mp4"
```

## Customization

### Change Duration
To change the chunk duration (e.g., to 30 minutes):

1. **Update all channels:**
```bash
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 "
cd /home/ubuntu/goondvr && \
sudo docker compose exec -T recorder sh -c 'cat conf/channels.json' | \
jq 'map(.max_duration = 30)' > /tmp/channels_updated.json && \
sudo cp /tmp/channels_updated.json conf/channels.json
"
```

2. **Restart recorder:**
```bash
ssh -i aws-secrets/aws-key.pem ubuntu@54.210.37.19 \
  "cd /home/ubuntu/goondvr && sudo docker compose restart recorder"
```

### Update Single Channel
Via Web UI:
1. Go to http://54.210.37.19:8080
2. Click on channel
3. Edit settings
4. Set "Max Duration" to desired minutes
5. Save

### Disable Chunking
Set `max_duration: 0` to record entire stream as single file:
```bash
jq 'map(.max_duration = 0)' conf/channels.json
```

## Alternative: File Size Based Splitting

You can also split by file size instead of duration:
```json
{
  "max_duration": 0,      // Disable duration-based
  "max_filesize": 2000    // Split at 2GB
}
```

Or use both (whichever threshold is reached first):
```json
{
  "max_duration": 45,     // 45 minutes
  "max_filesize": 3000    // OR 3GB
}
```

## Current Status

✅ **All 268 channels configured** with 45-minute chunks
✅ **System restarted** and applying new settings
✅ **Continuous recording** - no gaps between chunks
✅ **Automatic file naming** with sequence numbers
✅ **Compatible with all upload hosts** (GoFile, Streamtape, VOE.sx, TurboViPlay)

## Example Recording Session

For a 3-hour stream:
```
username_2026-04-30_10-00-00.mp4      (45 min, 1.2GB)
username_2026-04-30_10-00-00_1.mp4    (45 min, 1.3GB)
username_2026-04-30_10-00-00_2.mp4    (45 min, 1.2GB)
username_2026-04-30_10-00-00_3.mp4    (45 min, 1.3GB)
```

Each file will be:
- Uploaded to all configured hosts
- Stored in Supabase database
- Available in the web UI
- Automatically processed and finalized

## Notes

- **No data loss**: Switching happens seamlessly between segments
- **No gaps**: Recording is continuous across file boundaries
- **Automatic**: No manual intervention needed
- **Persistent**: Settings survive container restarts and deployments
- **Per-channel**: Can customize duration for specific channels if needed

## Troubleshooting

### Files Not Splitting
1. Check max_duration is set: `jq '.[0].max_duration' conf/channels.json`
2. Verify container restarted after config change
3. Check logs for "max filesize or duration exceeded" messages

### Sequence Numbers Not Incrementing
- This is normal if stream is shorter than 45 minutes
- Sequence only increments when threshold is reached

### Want Different Duration Per Channel
Edit specific channel in web UI or manually in channels.json:
```json
{
  "username": "special_channel",
  "max_duration": 60,  // 1 hour for this channel
  ...
}
```

## Related Files
- `channel/channel_file.go` - File switching logic
- `channel/channel_record.go` - Recording and segment handling
- `conf/channels.json` - Channel configuration
- `entity/entity.go` - Channel entity structure
