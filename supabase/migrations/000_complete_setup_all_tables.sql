-- Complete Supabase Setup — creates all tables with all columns
-- Run this entire file in your Supabase SQL Editor once.

-- ============================================================================
-- 1. VIDEO UPLOADS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS video_uploads (
    id SERIAL PRIMARY KEY,
    streamer_name TEXT NOT NULL,
    filename TEXT,
    gofile_link TEXT,
    turboviplay_link TEXT,
    voesx_link TEXT,
    streamtape_link TEXT,
    byse_link TEXT,
    sendcm_link TEXT,
    thumbnail_link TEXT,
    sprite_link TEXT,
    embed_url TEXT,
    filesize BIGINT,
    room_title TEXT,
    tags JSONB DEFAULT '[]',
    viewers INTEGER DEFAULT 0,
    resolution TEXT,
    framerate INTEGER DEFAULT 0,
    recorded_at TIMESTAMPTZ,
    upload_date TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_video_uploads_streamer_name ON video_uploads(streamer_name);
CREATE INDEX IF NOT EXISTS idx_video_uploads_upload_date ON video_uploads(upload_date DESC);
CREATE INDEX IF NOT EXISTS idx_video_uploads_recorded_at ON video_uploads(recorded_at DESC);

ALTER TABLE video_uploads ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "Allow all operations on video_uploads" ON video_uploads;
CREATE POLICY "Allow all operations on video_uploads" ON video_uploads
    FOR ALL USING (true) WITH CHECK (true);

COMMENT ON TABLE video_uploads IS 'Stores video upload links from all hosting services';

-- ============================================================================
-- 2. CHANNELS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS channels (
    id SERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    site TEXT NOT NULL DEFAULT 'chaturbate',
    is_paused BOOLEAN NOT NULL DEFAULT false,
    framerate INTEGER NOT NULL DEFAULT 30,
    resolution INTEGER NOT NULL DEFAULT 1080,
    pattern TEXT NOT NULL,
    max_duration INTEGER NOT NULL DEFAULT 30,
    max_filesize INTEGER NOT NULL DEFAULT 0,
    compress BOOLEAN NOT NULL DEFAULT false,
    created_at BIGINT NOT NULL,
    streamed_at BIGINT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channels_username ON channels(username);
CREATE INDEX IF NOT EXISTS idx_channels_site ON channels(site);

ALTER TABLE channels ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "Allow all operations on channels" ON channels;
CREATE POLICY "Allow all operations on channels" ON channels
    FOR ALL USING (true) WITH CHECK (true);

COMMENT ON TABLE channels IS 'Stores channel configurations for the recorder';

-- ============================================================================
-- 3. APP SETTINGS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

ALTER TABLE app_settings ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "Allow all operations on app_settings" ON app_settings;
CREATE POLICY "Allow all operations on app_settings" ON app_settings
    FOR ALL USING (true) WITH CHECK (true);

COMMENT ON TABLE app_settings IS 'Stores application settings (cookies, API keys, etc.)';

-- ============================================================================
-- 4. TUNNEL SESSIONS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS tunnel_sessions (
    id BIGSERIAL PRIMARY KEY,
    run_id INTEGER,
    url TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tunnel_sessions_active ON tunnel_sessions(is_active, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_tunnel_sessions_run_id ON tunnel_sessions(run_id);

ALTER TABLE tunnel_sessions ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "Allow all operations on tunnel_sessions" ON tunnel_sessions;
CREATE POLICY "Allow all operations on tunnel_sessions" ON tunnel_sessions
    FOR ALL USING (true) WITH CHECK (true);

COMMENT ON TABLE tunnel_sessions IS 'Tracks Cloudflare tunnel URLs for accessing the recorder UI';

-- Function to mark old tunnels as inactive when a new one starts
CREATE OR REPLACE FUNCTION mark_old_tunnels_inactive()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE tunnel_sessions
    SET is_active = FALSE
    WHERE id != NEW.id AND is_active = TRUE;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_mark_old_tunnels_inactive ON tunnel_sessions;
CREATE TRIGGER trigger_mark_old_tunnels_inactive
    AFTER INSERT ON tunnel_sessions
    FOR EACH ROW
    EXECUTE FUNCTION mark_old_tunnels_inactive();

-- View to get the current active tunnel
CREATE OR REPLACE VIEW current_tunnel AS
SELECT *
FROM tunnel_sessions
WHERE is_active = TRUE
ORDER BY started_at DESC
LIMIT 1;

-- ============================================================================
-- VERIFICATION
-- ============================================================================

-- Run these to verify:
-- SELECT 'video_uploads' AS table_name, COUNT(*) AS row_count FROM video_uploads
-- UNION ALL SELECT 'channels', COUNT(*) FROM channels
-- UNION ALL SELECT 'app_settings', COUNT(*) FROM app_settings
-- UNION ALL SELECT 'tunnel_sessions', COUNT(*) FROM tunnel_sessions;
