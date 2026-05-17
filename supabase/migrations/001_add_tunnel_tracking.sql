-- Add tunnel tracking tables for GitHub Actions workflow
-- Run this in your Supabase SQL Editor after running 000_complete_setup.sql

-- ============================================================================
-- TUNNEL SESSIONS TABLE (for tracking active Cloudflare tunnels)
-- ============================================================================

CREATE TABLE IF NOT EXISTS tunnel_sessions (
    id SERIAL PRIMARY KEY,
    run_id INTEGER NOT NULL UNIQUE,
    url TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create indexes for faster queries
CREATE INDEX IF NOT EXISTS idx_tunnel_sessions_run_id ON tunnel_sessions(run_id);
CREATE INDEX IF NOT EXISTS idx_tunnel_sessions_is_active ON tunnel_sessions(is_active);
CREATE INDEX IF NOT EXISTS idx_tunnel_sessions_started_at ON tunnel_sessions(started_at DESC);

-- Enable Row Level Security
ALTER TABLE tunnel_sessions ENABLE ROW LEVEL SECURITY;

-- Create policy to allow all operations
CREATE POLICY "Allow all operations on tunnel_sessions" ON tunnel_sessions
    FOR ALL
    USING (true)
    WITH CHECK (true);

-- Add comment
COMMENT ON TABLE tunnel_sessions IS 'Tracks active Cloudflare tunnel sessions from GitHub Actions runs';

-- ============================================================================
-- VERIFICATION QUERIES
-- ============================================================================

-- Run these to verify tables were created successfully:
-- SELECT COUNT(*) as tunnel_sessions_count FROM tunnel_sessions;
-- SELECT * FROM tunnel_sessions ORDER BY started_at DESC LIMIT 10;
