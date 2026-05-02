# GitHub Actions 24/7 Setup Guide (with Web UI Access!)

## Overview

This setup runs your recorder continuously 24/7 using GitHub Actions with 5-hour intervals, **and gives you a public web UI** accessible from anywhere via Cloudflare Tunnel!

## How It Works

- **5 scheduled runs per day** (every 5 hours): 00:00, 05:00, 10:00, 15:00, 20:00 UTC
- Each run lasts **5 hours** (300 minutes)
- **Web UI accessible via Cloudflare Tunnel** (free, no signup needed)
- **Public URL** generated each run (e.g., `https://random-name.trycloudflare.com`)
- Automatic restart after each session
- **Zero downtime** between runs

## Setup Instructions

### 1. Configure GitHub Secrets

Go to your repository → Settings → Secrets and variables → Actions → New repository secret

Add these secrets:

#### Required Secrets:

```
SETTINGS_JSON
```
Copy your entire `settings.json` file content here.

#### Optional Secrets (if using upload features):

```
SUPABASE_URL=https://your-project.supabase.co
SUPABASE_API_KEY=your-anon-or-service-key
DISCORD_WEBHOOK=https://discord.com/api/webhooks/...
TURBOVIPLAY_API_KEY=your-key
VOESX_API_KEY=your-key
STREAMTAPE_LOGIN=your-login
STREAMTAPE_API_KEY=your-key
```

#### Optional (if you have channels configured):

```
CHANNELS_JSON
```
Copy your `conf/channels.json` file content here.

### 2. Push to GitHub

```bash
git add .github/workflows/continuous-recording.yml
git commit -m "Add 24/7 GitHub Actions workflow"
git push origin main
```

### 3. Enable Workflow

1. Go to your repository on GitHub
2. Click **Actions** tab
3. Click **"I understand my workflows, go ahead and enable them"** (if needed)
4. Find **"Continuous Recording (24/7)"** workflow
5. Click **"Run workflow"** to start immediately (optional)

### 4. Monitor & Access Web UI

1. Go to **Actions** tab
2. Click on the running workflow
3. Open the **"Start Cloudflare Tunnel"** step
4. **Copy the public URL** (looks like `https://xyz-abc-123.trycloudflare.com`)
5. **Open that URL in your browser** - you'll see your dashboard!

The URL is:
- ✅ **Public** - accessible from any device
- ✅ **Secure** - HTTPS encrypted
- ✅ **Free** - no Cloudflare account needed
- ⏰ **Valid for 5 hours** - new URL generated each run

**Example:**
```
╔════════════════════════════════════════════════════════════╗
║                    🌐 WEB UI ACCESS                        ║
╚════════════════════════════════════════════════════════════╝

✅ Your dashboard is now accessible at:

   https://random-name-1234.trycloudflare.com

📱 This URL is public and works from anywhere!
⏰ Valid for 5 hours (until workflow ends)
🔄 New URL will be generated on next run
```

## Schedule Details

| Time (UTC) | Status |
|------------|--------|
| 00:00-05:00 | Recording |
| 05:00-10:00 | Recording |
| 10:00-15:00 | Recording |
| 15:00-20:00 | Recording |
| 20:00-00:00 | Recording |

**Total Coverage: 24/7 continuous**

## Features

✅ **Web UI accessible from anywhere** via Cloudflare Tunnel
✅ Public HTTPS URL (no VPN or port forwarding needed)
✅ Automatic restart every 5 hours
✅ No manual intervention needed
✅ Recordings uploaded to configured hosts
✅ Database tracking
✅ Discord notifications
✅ Artifact backup (1 day retention)
✅ Free (GitHub Actions free tier: 2,000 minutes/month for private repos, unlimited for public)

## GitHub Actions Free Tier

- **Public repos:** Unlimited minutes
- **Private repos:** 2,000 minutes/month free
- **Usage:** 5 hours/day × 30 days = 150 hours = 9,000 minutes/month

**Note:** For private repos, you'll need GitHub Pro or pay for additional minutes. Consider making the repo public or using a different approach.

## Advantages Over AWS EC2

| Feature | GitHub Actions | AWS EC2 |
|---------|---------------|---------|
| Cost | Free (public) / $0.008/min (private) | ~$10-30/month |
| Setup | Simple (1 YAML file) | Complex (SSH, Docker, etc.) |
| Maintenance | Zero | Manual updates |
| Scaling | Automatic | Manual |
| Logs | Built-in UI | SSH required |

## Troubleshooting

### Workflow not starting?
- Check if Actions are enabled in repo settings
- Verify the workflow file is in `.github/workflows/`
- Check branch name (should be `main` or `master`)

### Secrets not working?
- Verify secret names match exactly (case-sensitive)
- Check if secrets are set at repository level
- Re-save secrets if needed

### Build failing?
- Check Go version compatibility
- Verify `go.mod` is committed
- Check FFmpeg installation step

### Want to stop?
- Go to Actions → Running workflow → Cancel
- Or disable the workflow in Actions tab

## Manual Trigger

You can manually start a recording session anytime:

1. Go to **Actions** tab
2. Select **"Continuous Recording (24/7)"**
3. Click **"Run workflow"**
4. Select branch and click **"Run workflow"**

## Customization

### Change duration:
Edit `timeout-minutes: 300` (300 = 5 hours)

### Change schedule:
Edit the `cron` expressions:
```yaml
schedule:
  - cron: '0 0 * * *'    # Midnight UTC
  - cron: '0 6 * * *'    # 6 AM UTC
  # Add more as needed
```

### Disable auto-upload:
Remove `--enable-gofile-upload` flag from the run command

## Next Steps

1. ✅ Set up GitHub secrets
2. ✅ Push workflow file
3. ✅ Enable Actions
4. ✅ Run workflow manually (optional)
5. ✅ Monitor first run
6. ✅ Verify recordings are working

## Support

If you encounter issues:
1. Check workflow logs in Actions tab
2. Verify all secrets are set correctly
3. Test locally first: `go run . --port 8080`
4. Check FFmpeg is working: `ffmpeg -version`

---

**Status:** Ready to deploy
**Last Updated:** 2026-05-03
