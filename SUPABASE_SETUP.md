# Supabase Setup Guide

This guide will help you fix the 404 errors in your GitHub Actions workflow by setting up the required Supabase tables.

## Problem

Your GitHub Actions workflow is failing with:
```
curl: (22) The requested URL returned error: 404
Could not save tunnel URL to Supabase.
```

This happens because the workflow tries to save data to `tunnel_sessions` and `heartbeats` tables that don't exist in your Supabase database.

## Solution

### Step 1: Run the Database Migrations

1. Go to your Supabase project dashboard: https://supabase.com/dashboard
2. Navigate to **SQL Editor** in the left sidebar
3. Click **New Query**
4. Copy and paste the contents of `supabase/migrations/001_add_tunnel_tracking.sql`
5. Click **Run** to execute the SQL

This will create two new tables:
- `tunnel_sessions` - Tracks active Cloudflare tunnels from GitHub Actions
- `heartbeats` - Monitors GitHub Actions runner health

### Step 2: Get Your Supabase Credentials

1. In your Supabase dashboard, go to **Settings** → **API**
2. Copy the following values:
   - **Project URL** (e.g., `https://xxxxx.supabase.co`)
   - **anon/public key** (the long string under "Project API keys")

### Step 3: Add Secrets to GitHub

1. Go to your GitHub repository
2. Navigate to **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret** and add:
   - Name: `SUPABASE_URL`
   - Value: Your Project URL from Step 2
4. Click **New repository secret** again and add:
   - Name: `SUPABASE_API_KEY`
   - Value: Your anon/public key from Step 2

### Step 4: Update Local .env File (Optional)

If you want to test locally, update your `.env` file:

```env
SUPABASE_URL=https://your-project.supabase.co
SUPABASE_API_KEY=your_supabase_anon_key
```

Replace the placeholder values with your actual credentials from Step 2.

## Verification

After completing these steps:

1. Trigger a new GitHub Actions workflow run
2. The workflow should now successfully:
   - Save the tunnel URL to Supabase
   - Send periodic heartbeats
   - Update tunnel status on completion

You can verify the data in Supabase:
- Go to **Table Editor** in your Supabase dashboard
- Check the `tunnel_sessions` and `heartbeats` tables for new entries

## Troubleshooting

### Still getting 404 errors?

1. Verify the tables were created:
   ```sql
   SELECT * FROM tunnel_sessions LIMIT 1;
   SELECT * FROM heartbeats LIMIT 1;
   ```

2. Check that Row Level Security policies are set correctly:
   ```sql
   SELECT * FROM pg_policies WHERE tablename IN ('tunnel_sessions', 'heartbeats');
   ```

3. Verify your GitHub secrets are set correctly (no extra spaces or quotes)

### DNS resolution errors?

The `curl: (6) Could not resolve host` error is usually transient. The workflow will retry and should succeed on subsequent attempts.

## What These Tables Do

### tunnel_sessions
Tracks each GitHub Actions run's Cloudflare tunnel:
- `run_id` - GitHub Actions run number
- `url` - The Cloudflare tunnel URL (e.g., `https://xxx.trycloudflare.com`)
- `started_at` - When the tunnel was created
- `is_active` - Whether the tunnel is currently active
- `last_seen_at` - Last heartbeat timestamp

### heartbeats
Monitors runner health with periodic updates:
- `run_id` - GitHub Actions run number
- `timestamp` - When the heartbeat was sent
- `tunnel_url` - Current tunnel URL

This helps you monitor your recorder's uptime and access the web UI remotely.
