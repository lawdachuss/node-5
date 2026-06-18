package channel

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/teacat/chaturbate-dvr/config"
	"github.com/teacat/chaturbate-dvr/uploader"
)

const (
	thumbWidth        = 1280
	thumbHeight       = 720
	spriteFrames      = 16
	spriteCols        = 4
	spriteRows        = 4
	spriteFrameW      = 640
	spriteFrameH      = 360
	previewBaseFrames = 40
	previewWidth      = 320
	previewFPS        = 8
)

// generateThumbnail is the channel-scoped wrapper — logs go to the channel log.
func (ch *Channel) generateThumbnail(videoPath string) (thumbURL, spriteURL, previewURL string) {
	return generateThumbnailForFile(videoPath,
		func(f string, a ...interface{}) { ch.Info(f, a...) },
		func(f string, a ...interface{}) { ch.Error(f, a...) },
	)
}

// GenerateThumbnailForFile is a standalone thumbnail generator that can be
// called outside of a channel context (e.g. for pre-existing video files).
func GenerateThumbnailForFile(videoPath string) (thumbURL, spriteURL, previewURL string) {
	return generateThumbnailForFile(videoPath,
		func(f string, a ...interface{}) { log.Printf("[thumb] "+f, a...) },
		func(f string, a ...interface{}) { log.Printf("[thumb:err] "+f, a...) },
	)
}

// generateThumbnailForFile creates a static thumbnail (JPEG), a multi-frame sprite
// sheet (JPEG), and an animated WebP preview covering the full video duration. All
// three are uploaded to remote image hosts and the URLs returned.  Local
// temp files are always cleaned up.
//
// JPEG at quality 85 is used for thumbnail and sprite because:
//   - All image hosts support it (Pixhost, ImgBB, Catbox)
//   - mjpeg encoder is fast (minimal encoding lag)
//   - Small filesize with good visual quality
//
// Thumbnail, sprite, and preview run in parallel with independent timeouts:
//   - thumbnail: 5 min  (single-frame seek)
//   - sprite:    15 min (seeks through full video for long recordings)
//   - preview:   15 min (same seeks as sprite)
//
// Using separate contexts prevents one task from being killed prematurely
// when a long video causes another to exceed a shared short timeout.
func generateThumbnailForFile(videoPath string, info, errFn func(string, ...interface{})) (thumbURL, spriteURL, previewURL string) {
	ext := strings.ToLower(filepath.Ext(videoPath))
	if ext != ".mp4" && ext != ".mkv" && ext != ".ts" {
		return "", "", ""
	}

	st, err := os.Stat(videoPath)
	if err != nil {
		errFn("thumb: file not found %s: %v", filepath.Base(videoPath), err)
		return "", "", ""
	}
	// Skip files too small to contain video frames — ffmpeg returns
	// exit code -22 (EINVAL) on header-only fMP4 from failed streams.
	if st.Size() < 100*1024 {
		errFn("thumb: skipping %s: too small (%d bytes)", filepath.Base(videoPath), st.Size())
		return "", "", ""
	}

	baseName := filepath.Base(videoPath)

	// Probe video duration — short dedicated timeout.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer probeCancel()

	var dur float64
	config.AcquireFFmpeg()
	probeOut, probeErr := config.FFprobeCommandContext(probeCtx,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	).Output()
	config.ReleaseFFmpeg() // release immediately — the 3 goroutines below also need slots
	if probeErr == nil {
		var parseErr error
		dur, parseErr = strconv.ParseFloat(strings.TrimSpace(string(probeOut)), 64)
		if parseErr != nil {
			log.Printf("WARN: could not parse probe duration %q: %v", strings.TrimSpace(string(probeOut)), parseErr)
		}
	}

	// Compute the interval so we get exactly spriteFrames frames spread
	// evenly across the whole video.  Clamp to at least 0.1 s.
	interval := 10.0
	if dur > 0 {
		interval = dur / float64(spriteFrames)
		if interval < 0.1 {
			interval = 0.1
		}
	}

	thumbDone := make(chan string, 1)
	spriteDone := make(chan string, 1)
	previewDone := make(chan string, 1)

	// ── Single thumbnail (static frame near the 10% mark) ──────────────────
	// Independent 90-second context: seeking to a single frame is always fast.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC [thumb] generating thumbnail for %s: %v", baseName, r)
				thumbDone <- ""
			}
		}()
		thumbCtx, thumbCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer thumbCancel()

		thumbJPG := videoPath + ".thumb.jpg"
		defer os.Remove(thumbJPG)

		seekPos := "00:00:03"
		if dur > 0 && dur < 3 {
			seekPos = fmt.Sprintf("%.2f", dur*0.5)
		} else if dur > 0 {
			seekPos = fmt.Sprintf("%.2f", dur*0.1)
		}

		config.AcquireFFmpeg()
		defer config.ReleaseFFmpeg()
		err := config.FFmpegCommandContext(thumbCtx,
			"-y",
			"-ss", seekPos,
			"-i", videoPath,
			"-vframes", "1",
			"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
				thumbWidth, thumbHeight, thumbWidth, thumbHeight),
			"-c:v", "mjpeg",
			"-q:v", "5",
			thumbJPG,
		).Run()

		if err != nil {
			errFn("thumb: failed for %s: %v", baseName, err)
			thumbDone <- ""
			return
		}

		imgUploader := uploader.NewMultiImageUploader()
		if remoteURL, _, uploadErr := imgUploader.Upload(thumbJPG); uploadErr == nil {
			info("thumb: ✓ %s", baseName)
			thumbDone <- remoteURL
		} else {
			errFn("thumb: upload failed for %s: %v", baseName, uploadErr)
			thumbDone <- ""
		}
	}()

	// ── Sprite sheet (4×4 grid covering the full video duration) ───────────
	// Each frame is spriteFrameW×spriteFrameH px; total image is
	// (spriteCols*spriteFrameW) × (spriteRows*spriteFrameH) = 2560×1440.
	// Using 640×360 frames so HiDPI/Retina displays get sharp previews.
	//
	// Independent 15-minute context: for long recordings (1 h+), ffmpeg must
	// seek to 16 evenly-spaced positions, which can take several minutes on a
	// slow or resource-constrained host.  A short shared context would cause
	// SIGKILL ("signal: killed") and silently skip sprite generation.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC [sprite] generating sprite for %s: %v", baseName, r)
				spriteDone <- ""
			}
		}()
		spriteCtx, spriteCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer spriteCancel()

		spriteJPG := videoPath + ".sprite.jpg"
		defer os.Remove(spriteJPG)

		// fps=1/INTERVAL extracts one frame per interval.
		// scale with lanczos gives sharper results than the default bilinear.
		// pad keeps each tile at exactly spriteFrameW×spriteFrameH.
		// tile=COLSxROWS assembles them into the contact sheet.
		vf := fmt.Sprintf(
			"fps=1/%.4f,scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,tile=%dx%d",
			interval,
			spriteFrameW, spriteFrameH,
			spriteFrameW, spriteFrameH,
			spriteCols, spriteRows,
		)

		config.AcquireFFmpeg()
		defer config.ReleaseFFmpeg()
		err := config.FFmpegCommandContext(spriteCtx,
			"-y",
			"-i", videoPath,
			"-vf", vf,
			"-frames:v", "1",
			"-c:v", "mjpeg",
			"-q:v", "5",
			spriteJPG,
		).Run()

		if err != nil {
			errFn("sprite: failed for %s: %v", baseName, err)
			spriteDone <- ""
			return
		}

		imgUploader := uploader.NewMultiImageUploader()
		if remoteURL, _, uploadErr := imgUploader.Upload(spriteJPG); uploadErr == nil {
			info("sprite: ✓ %s", baseName)
			spriteDone <- remoteURL
		} else {
			errFn("sprite: upload failed for %s: %v", baseName, uploadErr)
			spriteDone <- ""
		}
	}()

	// ── Animated WebP preview covering the FULL video duration ──────────────
	// Each frame is 320px wide. WebP is far smaller than GIF at same quality.
	// fps is calculated so that the selected frame count is spread evenly across
	// the entire video.  Longer recordings get more frames for smoother playback:
	//   <1 min:   40 frames (1 frame per ~1.5s)
	//   1-10 min: 60 frames
	//   10-60+min: 80 frames
	// For short videos (<5 s) fps is capped at previewFPS (8) for smooth playback.
	// Same 15-minute timeout as the sprite for long videos.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC [preview] generating preview for %s: %v", baseName, r)
				previewDone <- ""
			}
		}()
		previewCtx, previewCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer previewCancel()

		previewWEBP := videoPath + ".preview.webp"
		defer os.Remove(previewWEBP)

		// More frames for longer videos = smoother preview.
		previewFrames := previewBaseFrames
		if dur > 60 {
			previewFrames = 60
		}
		if dur > 600 {
			previewFrames = 80
		}

		// Spread previewFrames across the full duration.
		// fps = frames / duration, capped at previewFPS for short videos.
		previewRate := float64(previewFPS) // cap at 8 fps
		if dur > 0 {
			if r := float64(previewFrames) / dur; r < previewRate {
				previewRate = r // slower fps to cover full duration
			}
		}
		// scale down to previewWidth, lanczos for sharp scaling.
		vf := fmt.Sprintf(
			"fps=%.4f,scale=%d:-1:flags=lanczos",
			previewRate,
			previewWidth,
		)

		config.AcquireFFmpeg()
		defer config.ReleaseFFmpeg()
		err := config.FFmpegCommandContext(previewCtx,
			"-y",
			"-i", videoPath,
			"-vf", vf,
			"-frames:v", strconv.Itoa(previewFrames),
			"-c:v", "libwebp_anim",
			"-quality", "75",
			"-loop", "0",
			previewWEBP,
		).Run()

		if err != nil {
			errFn("preview: failed for %s: %v", baseName, err)
			previewDone <- ""
			return
		}

		imgUploader := uploader.NewMultiImageUploader()
		if remoteURL, _, uploadErr := imgUploader.Upload(previewWEBP); uploadErr == nil {
			info("preview: ✓ %s", baseName)
			previewDone <- remoteURL
		} else {
			errFn("preview: upload failed for %s: %v", baseName, uploadErr)
			previewDone <- ""
		}
	}()

	thumbURL = <-thumbDone
	spriteURL = <-spriteDone
	previewURL = <-previewDone

	return thumbURL, spriteURL, previewURL
}
