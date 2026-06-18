# MiniDelectableService

A high-performance, always-on stream recorder and video manager for Chaturbate and Stripchat. Written in Go with a built-in web UI, GPU-accelerated compression, multi-host uploading, and crash-recoverable pipelines.

## Features

- **Multi-site recording** — Chaturbate and Stripchat via a pluggable `site.Site` interface
- **HLS segment downloading** — Downloads raw TS/M4S segments directly from CDN edges with automatic failover across multiple CDN regions (lax, fra, AMS, sin, hnd)
- **GPU-accelerated compression** — Automatic encoder detection: NVENC (NVIDIA) > AMF (AMD) > QSV (Intel) > VideoToolbox (macOS) > CPU fallback via mp4ff muxer
- **Multi-host uploading** — Parallel uploads to GoFile, Streamtape, VOE.sx, MixDrop with file-hash deduplication
- **Crash-recoverable pipeline** — Post-recording pipeline (Thumbnail → Metadata → Cleanup) persisted in Supabase, survives restarts
- **Thumbnail generation** — Static thumbnails (1280×720), sprite sheets (4×4 grid), and animated GIF previews (40 frames, 8fps)
- **Built-in web UI** — Dark-mode dashboard with live channel logs (SSE), video browser, and player with HLS support
- **Adaptive rate limiting** — Token-bucket rate limiter + circuit breaker for resilient API calls
- **Chrome TLS fingerprinting** — Uses httpcloak to spoof Chrome 146 TLS fingerprints, bypassing Cloudflare WAF
- **SOCKS5 proxy support** — Proxy rotation for CDN and API requests
- **Cloudflare tunnels** — One-command public access via cloudflared
- **Scheduled task persistence** — Windows Task Scheduler for auto-restart on reboot
- **File watcher** — fsnotify-based watcher for external video processing

## Quick Start

```bash
# Clone the repo
git clone https://github.com/YOUR_USERNAME/MiniDelectableService.git
cd MiniDelectableService

# Run automated setup (Windows)
.\setup.bat

# Or PowerShell
.\setup.ps1
```

The setup script installs all dependencies (FFmpeg, cloudflared, Go, Node.js, Python), compiles the binary, builds Tailwind CSS, and configures GitHub Actions deployment.

## Manual Setup

### Prerequisites

| Dependency | Purpose | Install |
|---|---|---|
| **Go 1.23+** | Build the binary | [go.dev](https://go.dev/dl/) |
| **FFmpeg** | Stream compression/muxing | [ffmpeg.org](https://ffmpeg.org/download.html) |
| **Node.js 20+** | Tailwind CSS build | [nodejs.org](https://nodejs.org/) |
| **Python 3** | Cookie refresh scripts | [python.org](https://www.python.org/) |
| **cloudflared** | Public tunnel access | [developers.cloudflare.com](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) |

### Build

```bash
# Install Go dependencies
go mod download

# Build Tailwind CSS
npm install
npm run build:css

# Compile binary
go build -o chaturbate-dvr.exe .
```

### Configure

```bash
# Copy environment template
cp .env.example .env

# Edit .env with your credentials
notepad .env
```

## Configuration

All configuration is done via environment variables (`.env` file) and JSON config files in `conf/`.

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `SUPABASE_URL` | Yes | Supabase project URL |
| `SUPABASE_API_KEY` | Yes | Supabase anon/service key |
| `PROXY_URL` | No | SOCKS5 proxy URL (`socks5://host:port`) |
| `USER_AGENT` | No | Custom User-Agent for API requests |

### Config Files

Place these in the `conf/` directory:

**`conf/settings.json`** — Global settings:
```json
{
  "cookies": "csrftoken=...; sessionid=...",
  "user_agent": "Mozilla/5.0 ...",
  "enable_gofile_upload": true,
  "enable_streamtape_upload": false,
  "enable_voesx_upload": false,
  "enable_mixdrop_upload": false,
}
```

**`conf/channels.json`** — Channel list:
```json
[
  { "username": "channel_name", "is_paused": false },
  { "username": "another_channel", "is_paused": true }
]
```

## Usage

### CLI Commands

```bash
# Start recording (default)
.\chaturbate-dvr.exe

# Start without tunnel
.\chaturbate-dvr.exe --no-tunnel

# Start with debug logging
.\chaturbate-dvr.exe --debug

# Specify output directory
.\chaturbate-dvr.exe --output-dir D:\videos

# Show version
.\chaturbate-dvr.exe --version
```

### Channel Management

```bash
# Add a channel
.\chaturbate-dvr.exe add <username>

# Remove a channel
.\chaturbate-dvr.exe remove <username>

# Pause a channel
.\chaturbate-dvr.exe pause <username>

# Resume a channel
.\chaturbate-dvr.exe resume <username>
```

### Other Commands

```bash
# Upload a local video file
.\chaturbate-dvr.exe upload <file_path>

# Run database migration
.\chaturbate-dvr.exe migrate

# Start a recording session
.\chaturbate-dvr.exe session <username>

# Recover orphaned files
.\chaturbate-dvr.exe orphan

# Start cloudflare tunnel only
.\chaturbate-dvr.exe tunnel
```

## Architecture

```
MiniDelectableService/
├── main.go                    # Entry point, CLI parsing, signal handling
├── config/                    # FFmpeg detection, concurrency config
├── entity/                    # Data models (ChannelConfig, Events)
├── server/                    # Global state, Supabase client, cache, disk monitor
├── manager/                   # Channel lifecycle, SSE, file watcher, sessions
├── channel/                   # Core recording logic
│   ├── channel.go             # Channel struct, context lifecycle
│   ├── channel_record.go      # HLS download, stream monitoring
│   ├── channel_file.go        # File creation, segment writing
│   ├── channel_compress.go    # GPU detection, MP4 muxing
│   ├── channel_upload.go      # Multi-host upload, dedup
│   ├── channel_thumbnail.go   # Thumbnail/sprite/GIF generation
│   ├── pipeline.go            # Crash-recoverable post-recording pipeline
│   └── upload_tracker.go      # Upload journal for crash recovery
├── chaturbate/                # Chaturbate API, HLS parsing, CDN failover
├── stripchat/                 # Stripchat API client
├── site/                      # Site interface (FetchStream, GetRoomStatus)
├── router/                    # Gin web server, API routes, SSE
│   ├── view/templates/        # Embedded HTML (Tailwind CSS, dark mode)
│   └── videos_handler.go      # Video browser with Supabase + local scan
├── uploader/                  # Upload hosts (GoFile, Streamtape, VOE.sx, etc.)
├── database/                  # Supabase REST client, migrations
├── internal/                  # HTTP client (httpcloak), rate limiter, errors
├── watcher/                   # fsnotify file watcher
├── scripts/                   # Diagnostic and utility scripts
├── docs/                      # Proxy/cookie/setup documentation
├── setup.bat                  # Automated Windows setup
└── setup.ps1                  # PowerShell equivalent
```

### Recording Pipeline

```
Chaturbate/Stripchat CDN
        │
        ▼
  ┌─────────────┐
  │ HLS Download │ ← Segment-by-segment with CDN failover
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │ Raw TS/M4S  │ ← Written to pending queue
  └──────┬──────┘
         │
         ▼
  ┌─────────────────┐
  │ GPU Compress    │ ← NVENC/AMF/QSV/VideoToolbox/CPU
  │ (MP4 Mux)       │
  └──────┬──────────┘
         │
         ▼
  ┌─────────────────┐
  │ Upload Pipeline │ ← GoFile, Streamtape, etc. (parallel)
  └──────┬──────────┘
         │
         ▼
  ┌─────────────────┐
  │ Thumbnail Gen   │ ← Static + Sprite + Animated GIF
  └──────┬──────────┘
         │
         ▼
  ┌─────────────────┐
  │ Save Metadata   │ ← Supabase database
  └──────┬──────────┘
         │
         ▼
  ┌─────────────────┐
  │ Cleanup         │ ← Remove temp files
  └─────────────────┘
```

## Web UI

The built-in web server (port 8080) provides:

- **Dashboard** — Live channel status, recording progress, disk usage
- **Live logs** — Real-time SSE stream per channel with filtering
- **Video browser** — Search, filter, and play recorded videos
- **Video player** — HLS.js-powered player with quality selection and theater mode

Access locally at `http://localhost:8080` or via Tailscale/Tunnel.

## Deployment

### GitHub Actions (Recommended)

The project includes a GitHub Actions workflow that provisions a Windows RDP runner:

1. Sets up a SOCKS5 proxy (Netherlands)
2. Installs FFmpeg, Tailscale, cloudflared
3. Clones the repo and runs `setup.bat`
4. Creates scheduled tasks for DVR and tunnel persistence
5. Provides RDP access via Tailscale

**Required Secrets:**
| Secret | Description |
|---|---|
| `env` | Contents of your `.env` file |
| `TAILSCALE_AUTH_KEY` | Tailscale auth key for networking |

**Trigger:**
```bash
gh workflow run secure-rdp.yml
```

### Manual Server Deployment

```bash
# On your server
git clone https://github.com/YOUR_USERNAME/MiniDelectableService.git
cd MiniDelectableService
.\setup.bat -NoAppStart

# DVR runs as a Windows Scheduled Task (auto-restarts on reboot)
```

## Tech Stack

| Component | Technology |
|---|---|
| **Language** | Go 1.23 |
| **Web Framework** | Gin |
| **Database** | Supabase (PostgreSQL REST API) |
| **CSS** | Tailwind CSS |
| **TLS Fingerprinting** | httpcloak (Chrome 146) |
| **Video Muxing** | mp4ff |
| **HLS Parsing** | Go standard library |
| **File Watching** | fsnotify |
| **Upload Hosts** | GoFile, Streamtape, VOE.sx, MixDrop |
| **Tunnel** | Cloudflare (cloudflared) |
| **Networking** | Tailscale |
| **CI/CD** | GitHub Actions |

## License

MIT License — Copyright 2024 TeaCat

See [LICENSE](LICENSE) for details.
