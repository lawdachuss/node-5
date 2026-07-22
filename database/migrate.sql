-- Pulled from live Supabase project rvbuzyljrwsxfxijotdf (ap-southeast-1) on 2026-07-22

-- Sequences (owned by their respective tables via SERIAL)
CREATE SEQUENCE IF NOT EXISTS comment_likes_id_seq;
CREATE SEQUENCE IF NOT EXISTS comments_id_seq;
CREATE SEQUENCE IF NOT EXISTS performer_follows_id_seq;
CREATE SEQUENCE IF NOT EXISTS reactions_id_seq;
CREATE SEQUENCE IF NOT EXISTS requests_id_seq;
CREATE SEQUENCE IF NOT EXISTS saved_videos_id_seq;
CREATE SEQUENCE IF NOT EXISTS user_collection_items_id_seq;
CREATE SEQUENCE IF NOT EXISTS user_notification_preferences_id_seq;
CREATE SEQUENCE IF NOT EXISTS user_notifications_id_seq;
CREATE SEQUENCE IF NOT EXISTS watch_history_id_seq;
CREATE SEQUENCE IF NOT EXISTS watch_later_items_id_seq;

-- Tables
CREATE TABLE IF NOT EXISTS app_settings (
    key VARCHAR NOT NULL PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS channel_assignments (
    username TEXT NOT NULL,
    site TEXT NOT NULL DEFAULT 'chaturbate',
    assigned_node TEXT REFERENCES nodes(node_id),
    status TEXT NOT NULL DEFAULT 'unassigned',
    is_live BOOLEAN NOT NULL DEFAULT false,
    live_checked_at TIMESTAMPTZ,
    assigned_at TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ,
    framerate INTEGER NOT NULL DEFAULT 60,
    resolution INTEGER NOT NULL DEFAULT 2160,
    pattern TEXT NOT NULL DEFAULT '',
    max_duration INTEGER NOT NULL DEFAULT 60,
    max_filesize INTEGER NOT NULL DEFAULT 0,
    compress BOOLEAN NOT NULL DEFAULT false,
    min_duration_before_upload INTEGER NOT NULL DEFAULT 1200,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_recorded_at TIMESTAMPTZ,
    PRIMARY KEY (username, site)
);

CREATE TABLE IF NOT EXISTS channels (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    username VARCHAR NOT NULL UNIQUE,
    is_paused BOOLEAN DEFAULT false,
    framerate INTEGER DEFAULT 30,
    resolution INTEGER DEFAULT 1080,
    pattern TEXT DEFAULT 'videos/{{.Username}}_{{.Year}}-{{.Month}}-{{.Day}}_{{.Hour}}-{{.Minute}}-{{.Second}}{{if .Sequence}}_{{.Sequence}}{{end}}',
    max_duration INTEGER DEFAULT 0,
    max_filesize INTEGER DEFAULT 0,
    compress BOOLEAN DEFAULT false,
    created_at BIGINT NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comment_likes (
    id INTEGER NOT NULL DEFAULT nextval('comment_likes_id_seq') PRIMARY KEY,
    comment_id INTEGER NOT NULL,
    session_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
    id INTEGER NOT NULL DEFAULT nextval('comments_id_seq') PRIMARY KEY,
    recording_id TEXT NOT NULL,
    parent_id INTEGER,
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    session_id TEXT,
    deleted BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS disk_usage (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    total_bytes BIGINT NOT NULL,
    used_bytes BIGINT NOT NULL,
    free_bytes BIGINT NOT NULL,
    percent_used INTEGER NOT NULL,
    recorded_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS nodes (
    node_id TEXT NOT NULL PRIMARY KEY,
    hostname TEXT NOT NULL DEFAULT '',
    instance_label TEXT NOT NULL DEFAULT '',
    software_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    current_load INTEGER NOT NULL DEFAULT 0,
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT now(),
    web_url TEXT NOT NULL DEFAULT '',
    session_deadline TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS performer_follows (
    id INTEGER NOT NULL DEFAULT nextval('performer_follows_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    performer_username TEXT NOT NULL,
    followed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pipeline_states (
    file_hash TEXT NOT NULL PRIMARY KEY,
    file_path TEXT NOT NULL,
    filename TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    file_size BIGINT DEFAULT 0,
    current_stage TEXT NOT NULL DEFAULT 'thumbnail_upload',
    failed BOOLEAN DEFAULT false,
    last_error TEXT DEFAULT '',
    thumb_url TEXT DEFAULT '',
    sprite_url TEXT DEFAULT '',
    preview_url TEXT DEFAULT '',
    embed_url TEXT DEFAULT '',
    links TEXT DEFAULT '{}',
    retries INTEGER NOT NULL DEFAULT 0,
    node_id TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pool_autopilot (
    username TEXT NOT NULL PRIMARY KEY,
    gender TEXT NOT NULL,
    viewers INTEGER NOT NULL DEFAULT 0,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS preview_images (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    recording_id UUID REFERENCES recordings(id),
    filename VARCHAR NOT NULL UNIQUE,
    thumbnail_url TEXT,
    sprite_url TEXT,
    preview_url TEXT,
    sprite_vtt_url TEXT,
    instance_id TEXT NOT NULL DEFAULT 'default',
    uploaded_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reactions (
    id INTEGER NOT NULL DEFAULT nextval('reactions_id_seq') PRIMARY KEY,
    recording_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS recordings (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    channel_id UUID REFERENCES channels(id),
    username VARCHAR NOT NULL,
    filename VARCHAR NOT NULL UNIQUE,
    timestamp TIMESTAMPTZ NOT NULL,
    room_title TEXT,
    tags TEXT[],
    viewers INTEGER DEFAULT 0,
    resolution VARCHAR,
    framerate INTEGER,
    filesize BIGINT DEFAULT 0,
    duration DOUBLE PRECISION DEFAULT 0,
    gender VARCHAR,
    thumbnail_url TEXT,
    sprite_url TEXT,
    embed_url TEXT,
    preview_url TEXT,
    seekstreaming_poster_url TEXT,
    seekstreaming_preview_url TEXT,
    sprite_vtt_url TEXT,
    instance_id TEXT NOT NULL DEFAULT 'default',
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS requests (
    id INTEGER NOT NULL DEFAULT nextval('requests_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT 'chaturbate',
    performer_username TEXT,
    stream_link TEXT,
    notes TEXT,
    priority TEXT NOT NULL DEFAULT 'normal',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS saved_videos (
    id INTEGER NOT NULL DEFAULT nextval('saved_videos_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    recording_id TEXT NOT NULL,
    metadata TEXT,
    saved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tunnels (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    url TEXT NOT NULL,
    run_id INTEGER,
    is_active BOOLEAN DEFAULT true,
    instance_id TEXT NOT NULL DEFAULT 'default',
    created_at TIMESTAMPTZ DEFAULT now(),
    expires_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS upload_journal (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    file_hash TEXT NOT NULL,
    filename TEXT NOT NULL,
    host TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_msg TEXT,
    file_size BIGINT,
    instance_id TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS upload_links (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    recording_id UUID,
    host VARCHAR NOT NULL,
    url TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT 'default',
    uploaded_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_collection_items (
    id INTEGER NOT NULL DEFAULT nextval('user_collection_items_id_seq') PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES user_collections(id),
    recording_id TEXT NOT NULL,
    metadata TEXT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_collections (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_notification_preferences (
    id INTEGER NOT NULL DEFAULT nextval('user_notification_preferences_id_seq') PRIMARY KEY,
    user_id UUID NOT NULL,
    notification_type VARCHAR NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    email_enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_notifications (
    id INTEGER NOT NULL DEFAULT nextval('user_notifications_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    related_id TEXT,
    is_read BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_profiles (
    user_id TEXT NOT NULL PRIMARY KEY,
    display_name TEXT,
    avatar_url TEXT,
    bio TEXT,
    username TEXT UNIQUE,
    email TEXT,
    sound_enabled BOOLEAN NOT NULL DEFAULT true,
    vibration_enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL PRIMARY KEY,
    role TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watch_history (
    id INTEGER NOT NULL DEFAULT nextval('watch_history_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    recording_id TEXT NOT NULL,
    metadata TEXT,
    progress_seconds INTEGER NOT NULL DEFAULT 0,
    duration_seconds INTEGER,
    watched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watch_later_items (
    id INTEGER NOT NULL DEFAULT nextval('watch_later_items_id_seq') PRIMARY KEY,
    user_id TEXT NOT NULL,
    recording_id TEXT NOT NULL,
    metadata TEXT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_ca_assigned_node ON channel_assignments(assigned_node);
CREATE INDEX IF NOT EXISTS idx_ca_heartbeat ON channel_assignments(last_heartbeat);
CREATE INDEX IF NOT EXISTS idx_ca_islive ON channel_assignments(is_live);
CREATE INDEX IF NOT EXISTS idx_ca_last_recorded ON channel_assignments(last_recorded_at);
CREATE INDEX IF NOT EXISTS idx_ca_status ON channel_assignments(status);
CREATE INDEX IF NOT EXISTS idx_channels_created_at ON channels(created_at);
CREATE INDEX IF NOT EXISTS idx_comment_likes_comment ON comment_likes(comment_id);
CREATE INDEX IF NOT EXISTS idx_comments_recording ON comments(recording_id);
CREATE INDEX IF NOT EXISTS idx_disk_usage_recorded_at ON disk_usage(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_nodes_heartbeat ON nodes(last_heartbeat);
CREATE INDEX IF NOT EXISTS idx_nodes_session_deadline ON nodes(session_deadline);
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
CREATE INDEX IF NOT EXISTS idx_performer_follows_user ON performer_follows(user_id);
CREATE INDEX IF NOT EXISTS idx_pool_autopilot_gender ON pool_autopilot(gender);
CREATE INDEX IF NOT EXISTS idx_preview_images_instance ON preview_images(instance_id);
CREATE INDEX IF NOT EXISTS idx_preview_images_recording_id ON preview_images(recording_id);
CREATE INDEX IF NOT EXISTS idx_reactions_recording ON reactions(recording_id);
CREATE INDEX IF NOT EXISTS idx_recordings_channel_id ON recordings(channel_id);
CREATE INDEX IF NOT EXISTS idx_recordings_gender ON recordings(gender);
CREATE INDEX IF NOT EXISTS idx_recordings_instance ON recordings(instance_id);
CREATE INDEX IF NOT EXISTS idx_recordings_timestamp ON recordings("timestamp" DESC);
CREATE INDEX IF NOT EXISTS idx_recordings_username ON recordings(username);
CREATE INDEX IF NOT EXISTS idx_requests_user ON requests(user_id);
CREATE INDEX IF NOT EXISTS idx_saved_videos_user ON saved_videos(user_id);
CREATE INDEX IF NOT EXISTS idx_tunnels_active ON tunnels(is_active, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tunnels_instance ON tunnels(instance_id);
CREATE INDEX IF NOT EXISTS idx_tunnels_run_id ON tunnels(run_id);
CREATE INDEX IF NOT EXISTS idx_upload_journal_hash ON upload_journal(file_hash);
CREATE INDEX IF NOT EXISTS idx_upload_journal_status ON upload_journal(status);
CREATE INDEX IF NOT EXISTS idx_upload_links_host ON upload_links(host);
CREATE INDEX IF NOT EXISTS idx_upload_links_instance ON upload_links(instance_id);
CREATE INDEX IF NOT EXISTS idx_upload_links_recording_id ON upload_links(recording_id);
CREATE INDEX IF NOT EXISTS idx_collection_items_collection ON user_collection_items(collection_id);
CREATE INDEX IF NOT EXISTS idx_collections_user ON user_collections(user_id);
CREATE INDEX IF NOT EXISTS idx_notification_preferences_user ON user_notification_preferences(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON user_notifications(user_id, is_read);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON user_notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_watch_history_user ON watch_history(user_id);
CREATE INDEX IF NOT EXISTS idx_watch_later_user ON watch_later_items(user_id);

-- Unique indexes (not PK)
CREATE UNIQUE INDEX IF NOT EXISTS performer_follows_uniq ON performer_follows(user_id, performer_username);
CREATE UNIQUE INDEX IF NOT EXISTS upload_journal_file_hash_host_key ON upload_journal(file_hash, host);
CREATE UNIQUE INDEX IF NOT EXISTS upload_links_recording_host_unique ON upload_links(recording_id, host);
CREATE UNIQUE INDEX IF NOT EXISTS collection_items_uniq ON user_collection_items(collection_id, recording_id);
CREATE UNIQUE INDEX IF NOT EXISTS user_notification_preferences_user_id_notification_type_key ON user_notification_preferences(user_id, notification_type);
CREATE UNIQUE INDEX IF NOT EXISTS reactions_recording_session_uniq ON reactions(recording_id, session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_requests_user_platform_performer ON requests(user_id, platform, COALESCE(performer_username, ''), COALESCE(stream_link, ''));
CREATE UNIQUE INDEX IF NOT EXISTS saved_videos_uniq ON saved_videos(user_id, recording_id);
CREATE UNIQUE INDEX IF NOT EXISTS watch_history_uniq ON watch_history(user_id, recording_id);
CREATE UNIQUE INDEX IF NOT EXISTS watch_later_uniq ON watch_later_items(user_id, recording_id);

-- Views
CREATE OR REPLACE VIEW channel_statistics AS
 SELECT c.username,
    c.is_paused,
    count(r.id) AS total_recordings,
    sum(r.filesize) AS total_filesize_bytes,
    max(r."timestamp") AS last_recording_at,
    avg(r.viewers) AS avg_viewers,
    c.created_at,
    c.updated_at
   FROM channels c
     LEFT JOIN recordings r ON c.username::text = r.username::text
  GROUP BY c.id, c.username, c.is_paused, c.created_at, c.updated_at;

CREATE OR REPLACE VIEW recordings_with_links AS
 SELECT r.id,
    r.channel_id,
    r.username,
    r.filename,
    r."timestamp",
    r.room_title,
    r.tags,
    r.viewers,
    r.resolution,
    r.framerate,
    r.filesize,
    r.duration,
    r.gender,
    r.thumbnail_url,
    r.sprite_url,
    r.embed_url,
    r.preview_url,
    r.sprite_vtt_url,
    r.instance_id,
    r.created_at,
    r.updated_at,
    (NULLIF(jsonb_object_agg(ul.host, ul.url) FILTER (WHERE ul.host IS NOT NULL), '{}'::jsonb))::json AS links
   FROM recordings r
     LEFT JOIN upload_links ul ON r.id = ul.recording_id
  GROUP BY r.id;

-- Functions
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION update_streamers_updated_at()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION update_user_channels_updated_at()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION claim_channel(p_channel_id text, p_repo text)
RETURNS TABLE(recording_id uuid, token uuid)
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
DECLARE
    v_recording_id uuid;
    v_token uuid;
BEGIN
    INSERT INTO recordings (channel_id, repo_node, lock_token, lock_expires_at)
    VALUES (p_channel_id, p_repo, gen_random_uuid(), now() + interval '90 seconds')
    ON CONFLICT (channel_id, active) WHERE active = true DO NOTHING
    RETURNING id, lock_token INTO v_recording_id, v_token;
    IF v_recording_id IS NULL THEN
        UPDATE recordings
        SET repo_node = p_repo,
            lock_token = gen_random_uuid(),
            lock_expires_at = now() + interval '90 seconds',
            started_at = now()
        WHERE channel_id = p_channel_id
          AND active = true
          AND (lock_expires_at IS NULL OR lock_expires_at < now())
        RETURNING id, lock_token INTO v_recording_id, v_token;
    END IF;
    RETURN QUERY SELECT v_recording_id, v_token;
END;
$$;

CREATE OR REPLACE FUNCTION claim_channels(p_node_id text, p_limit integer)
RETURNS SETOF channel_assignments
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  RETURN QUERY
  WITH candidates AS (
    SELECT username, site
    FROM public.channel_assignments
    WHERE assigned_node IS NULL
      AND status = 'unassigned'
    ORDER BY username ASC
    LIMIT p_limit
    FOR UPDATE SKIP LOCKED
  )
  UPDATE public.channel_assignments ca
  SET assigned_node  = p_node_id,
      status         = 'claimed',
      assigned_at    = NOW(),
      last_heartbeat = NOW()
  FROM candidates c
  WHERE ca.username = c.username
    AND ca.site = c.site
  RETURNING ca.*;
END;
$$;

CREATE OR REPLACE FUNCTION claim_specific_channel(p_username text, p_site text, p_node_id text)
RETURNS SETOF channel_assignments
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  RETURN QUERY
  WITH candidate AS (
    SELECT username, site
    FROM public.channel_assignments
    WHERE username = p_username
      AND site = p_site
      AND assigned_node IS NULL
      AND status = 'unassigned'
    FOR UPDATE SKIP LOCKED
  )
  UPDATE public.channel_assignments ca
  SET assigned_node  = p_node_id,
      status         = 'claimed',
      assigned_at    = NOW(),
      last_heartbeat = NOW()
  FROM candidate c
  WHERE ca.username = c.username
    AND ca.site = c.site
  RETURNING ca.*;
END;
$$;

CREATE OR REPLACE FUNCTION clean_stale_locks()
RETURNS SETOF uuid
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
    RETURN QUERY
    UPDATE recordings
    SET active = false,
        ended_at = now(),
        lock_token = null,
        lock_expires_at = null
    WHERE active = true
      AND lock_expires_at < now()
    RETURNING id;
END;
$$;

CREATE OR REPLACE FUNCTION finalize_recording(p_recording_id uuid, p_token uuid)
RETURNS void
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
    UPDATE recordings
    SET active = false,
        ended_at = now(),
        lock_token = null,
        lock_expires_at = null
    WHERE id = p_recording_id
      AND lock_token = p_token;
END;
$$;

CREATE OR REPLACE FUNCTION increment_video_views(video_id text)
RETURNS void
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  UPDATE video_uploads SET views = COALESCE(views, 0) + 1 WHERE id = video_id;
END;
$$;

CREATE OR REPLACE FUNCTION mark_old_tunnels_inactive()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
    UPDATE tunnel_sessions
    SET is_active = FALSE
    WHERE id != NEW.id AND is_active = TRUE;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION notify_requesters_on_upload()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
      DECLARE
        v_username text;
      BEGIN
        SELECT r.username INTO v_username
        FROM public.recordings r
        WHERE r.id = NEW.recording_id;

        IF v_username IS NULL THEN
          RETURN NEW;
        END IF;

        INSERT INTO public.user_notifications (user_id, type, message, related_id, is_read, created_at)
        SELECT
          rq.user_id,
          'recording_available',
          'A new recording of @' || rq.performer_username || ' on ' || rq.platform || ' is now available in the archive!',
          NEW.recording_id::text,
          false,
          NOW()
        FROM public.requests rq
        LEFT JOIN public.user_notification_preferences unp
          ON unp.user_id = rq.user_id
          AND unp.notification_type = 'recording_available'
        WHERE rq.performer_username IS NOT NULL
          AND LOWER(rq.performer_username) = LOWER(v_username)
          AND rq.status IN ('pending', 'approved')
          AND (unp.enabled IS NULL OR unp.enabled = true)
          AND NOT EXISTS (
            SELECT 1 FROM public.user_notifications un
            WHERE un.user_id = rq.user_id
              AND un.type = 'recording_available'
              AND un.related_id = NEW.recording_id::text
          );

        RETURN NEW;
      END;
      $$;

CREATE OR REPLACE FUNCTION reassign_channel(p_username text, p_site text, p_from_node text, p_to_node text)
RETURNS SETOF channel_assignments
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
  RETURN QUERY
  WITH cand AS (
    SELECT username, site
    FROM channel_assignments
    WHERE username = p_username
      AND site = p_site
      AND assigned_node = p_from_node
    FOR UPDATE SKIP LOCKED
  )
  UPDATE channel_assignments ca
  SET assigned_node  = p_to_node,
      status         = 'claimed',
      assigned_at    = NOW(),
      last_heartbeat = NOW()
  FROM cand c
  WHERE ca.username = c.username AND ca.site = c.site
  RETURNING ca.*;
END;
$$;

CREATE OR REPLACE FUNCTION renew_lock(p_recording_id uuid, p_token uuid)
RETURNS boolean
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $$
BEGIN
    UPDATE recordings
    SET lock_expires_at = now() + interval '90 seconds'
    WHERE id = p_recording_id
      AND active = true
      AND lock_token = p_token;
    RETURN FOUND;
END;
$$;

CREATE OR REPLACE FUNCTION resolve_username(p_username text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_email TEXT;
BEGIN
  SELECT up.email INTO v_email
  FROM public.user_profiles up
  WHERE LOWER(up.username) = LOWER(p_username)
  LIMIT 1;

  IF v_email IS NOT NULL THEN
    RETURN v_email;
  END IF;

  SELECT au.email INTO v_email
  FROM auth.users au
  WHERE
    LOWER(au.raw_user_meta_data->>'display_name') = LOWER(p_username)
    OR LOWER(au.raw_user_meta_data->>'username') = LOWER(p_username)
  LIMIT 1;

  RETURN v_email;
END;
$$;

CREATE OR REPLACE FUNCTION rls_auto_enable()
RETURNS event_trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $$
DECLARE
  cmd record;
BEGIN
  FOR cmd IN
    SELECT *
    FROM pg_event_trigger_ddl_commands()
    WHERE command_tag IN ('CREATE TABLE', 'CREATE TABLE AS', 'SELECT INTO')
      AND object_type IN ('table','partitioned table')
  LOOP
     IF cmd.schema_name IS NOT NULL AND cmd.schema_name IN ('public') AND cmd.schema_name NOT IN ('pg_catalog','information_schema') AND cmd.schema_name NOT LIKE 'pg_toast%' AND cmd.schema_name NOT LIKE 'pg_temp%' THEN
      BEGIN
        EXECUTE format('alter table if exists %s enable row level security', cmd.object_identity);
        RAISE LOG 'rls_auto_enable: enabled RLS on %', cmd.object_identity;
      EXCEPTION
        WHEN OTHERS THEN
          RAISE LOG 'rls_auto_enable: failed to enable RLS on %', cmd.object_identity;
      END;
     ELSE
        RAISE LOG 'rls_auto_enable: skip % (either system schema or not in enforced list: %.)', cmd.object_identity, cmd.schema_name;
     END IF;
  END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION upsert_upload_link(
  p_recording_id TEXT,
  p_host TEXT,
  p_url TEXT,
  p_instance_id TEXT DEFAULT 'default'
)
RETURNS SETOF upload_links
LANGUAGE plpgsql
AS $$
BEGIN
  RETURN QUERY
  INSERT INTO upload_links (recording_id, host, url, instance_id)
  VALUES (p_recording_id::UUID, p_host, p_url, p_instance_id)
  ON CONFLICT (recording_id, host) DO UPDATE SET url = EXCLUDED.url, uploaded_at = NOW()
  RETURNING *;
END;
$$;

CREATE OR REPLACE FUNCTION upsert_upload_links(p_links JSONB)
RETURNS SETOF upload_links
LANGUAGE plpgsql
AS $$
BEGIN
  RETURN QUERY
  WITH link_data AS (
    SELECT 
      (elem->>'recording_id')::UUID AS recording_id,
      elem->>'host' AS host,
      elem->>'url' AS url,
      COALESCE(elem->>'instance_id', 'default') AS instance_id
    FROM jsonb_array_elements(p_links) AS elem
  )
  INSERT INTO upload_links (recording_id, host, url, instance_id)
  SELECT recording_id, host, url, instance_id
  FROM link_data
  ON CONFLICT (recording_id, host) 
  DO UPDATE SET 
    url = EXCLUDED.url, 
    uploaded_at = NOW()
  RETURNING *;
END;
$$;

CREATE OR REPLACE FUNCTION get_upload_links(p_recording_id TEXT)
RETURNS SETOF upload_links
LANGUAGE plpgsql
AS $$
BEGIN
  RETURN QUERY
  SELECT * FROM upload_links WHERE recording_id = p_recording_id::UUID;
END;
$$;

CREATE OR REPLACE FUNCTION delete_upload_links(p_recording_id TEXT)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
  DELETE FROM upload_links WHERE recording_id = p_recording_id::UUID;
END;
$$;

-- Triggers
DROP TRIGGER IF EXISTS update_upload_links_updated_at ON upload_links;
DROP TRIGGER IF EXISTS trigger_notify_requesters_on_upload ON upload_links;
CREATE TRIGGER trigger_notify_requesters_on_upload
  AFTER INSERT ON public.upload_links
  FOR EACH ROW EXECUTE FUNCTION notify_requesters_on_upload();

DROP TRIGGER IF EXISTS update_app_settings_updated_at ON app_settings;
CREATE TRIGGER update_app_settings_updated_at
  BEFORE UPDATE ON public.app_settings
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_channel_assignments_updated_at ON channel_assignments;
CREATE TRIGGER update_channel_assignments_updated_at
  BEFORE UPDATE ON public.channel_assignments
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_channels_updated_at ON channels;
CREATE TRIGGER update_channels_updated_at
  BEFORE UPDATE ON public.channels
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_nodes_updated_at ON nodes;
CREATE TRIGGER update_nodes_updated_at
  BEFORE UPDATE ON public.nodes
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_recordings_updated_at ON recordings;
CREATE TRIGGER update_recordings_updated_at
  BEFORE UPDATE ON public.recordings
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- RLS (Row Level Security)
ALTER TABLE app_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE channel_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment_likes ENABLE ROW LEVEL SECURITY;
ALTER TABLE comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE disk_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE nodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE performer_follows ENABLE ROW LEVEL SECURITY;
ALTER TABLE pipeline_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE pool_autopilot ENABLE ROW LEVEL SECURITY;
ALTER TABLE preview_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE reactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE recordings ENABLE ROW LEVEL SECURITY;
ALTER TABLE requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE saved_videos ENABLE ROW LEVEL SECURITY;
ALTER TABLE tunnels ENABLE ROW LEVEL SECURITY;
ALTER TABLE upload_journal ENABLE ROW LEVEL SECURITY;
ALTER TABLE upload_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_collection_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_collections ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch_later_items ENABLE ROW LEVEL SECURITY;

-- RLS policies (public read for all tables)
DROP POLICY IF EXISTS public_read_app_settings ON app_settings;
CREATE POLICY public_read_app_settings ON app_settings FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_channel_assignments ON channel_assignments;
CREATE POLICY public_read_channel_assignments ON channel_assignments FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_channels ON channels;
CREATE POLICY public_read_channels ON channels FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_comment_likes ON comment_likes;
CREATE POLICY public_read_comment_likes ON comment_likes FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_comments ON comments;
CREATE POLICY public_read_comments ON comments FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_disk_usage ON disk_usage;
CREATE POLICY public_read_disk_usage ON disk_usage FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_nodes ON nodes;
CREATE POLICY public_read_nodes ON nodes FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_performer_follows ON performer_follows;
CREATE POLICY public_read_performer_follows ON performer_follows FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_pipeline_states ON pipeline_states;
CREATE POLICY public_read_pipeline_states ON pipeline_states FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_pool_autopilot ON pool_autopilot;
CREATE POLICY public_read_pool_autopilot ON pool_autopilot FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_preview_images ON preview_images;
CREATE POLICY public_read_preview_images ON preview_images FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_reactions ON reactions;
CREATE POLICY public_read_reactions ON reactions FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_recordings ON recordings;
CREATE POLICY public_read_recordings ON recordings FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_requests ON requests;
CREATE POLICY public_read_requests ON requests FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_saved_videos ON saved_videos;
CREATE POLICY public_read_saved_videos ON saved_videos FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_tunnels ON tunnels;
CREATE POLICY public_read_tunnels ON tunnels FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_upload_journal ON upload_journal;
CREATE POLICY public_read_upload_journal ON upload_journal FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_upload_links ON upload_links;
CREATE POLICY public_read_upload_links ON upload_links FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_user_collection_items ON user_collection_items;
CREATE POLICY public_read_user_collection_items ON user_collection_items FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_user_collections ON user_collections;
CREATE POLICY public_read_user_collections ON user_collections FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_user_notifications ON user_notifications;
CREATE POLICY public_read_user_notifications ON user_notifications FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_user_profiles ON user_profiles;
CREATE POLICY public_read_user_profiles ON user_profiles FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_user_roles ON user_roles;
CREATE POLICY public_read_user_roles ON user_roles FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_watch_history ON watch_history;
CREATE POLICY public_read_watch_history ON watch_history FOR SELECT USING (true);

DROP POLICY IF EXISTS public_read_watch_later_items ON watch_later_items;
CREATE POLICY public_read_watch_later_items ON watch_later_items FOR SELECT USING (true);

-- Grant permissions to anon role
ALTER TABLE public.app_settings OWNER TO anon;
ALTER TABLE public.channel_assignments OWNER TO anon;
ALTER TABLE public.channels OWNER TO anon;
ALTER TABLE public.disk_usage OWNER TO anon;
ALTER TABLE public.nodes OWNER TO anon;
ALTER TABLE public.pipeline_states OWNER TO anon;
ALTER TABLE public.preview_images OWNER TO anon;
ALTER TABLE public.recordings OWNER TO anon;
ALTER TABLE public.tunnels OWNER TO anon;
ALTER TABLE public.upload_journal OWNER TO anon;
ALTER TABLE public.upload_links OWNER TO anon;

GRANT ALL ON ALL TABLES IN SCHEMA public TO anon;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO anon;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO anon;
