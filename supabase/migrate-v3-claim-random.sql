-- Migration v3: Channel distribution fixes
-- 
-- Fix 1: Randomize claim order so no node gets priority on alphabetically-first channels.
--   Old: ORDER BY username ASC (deterministic — earliest-starting node wins A-channels)
--   New: ORDER BY RANDOM() (each claim cycle gives every node a fair random subset)

CREATE OR REPLACE FUNCTION claim_channels(p_node_id TEXT, p_limit INT)
RETURNS SETOF channel_assignments
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    WITH picked AS (
        SELECT ca.username, ca.site
        FROM channel_assignments ca
        WHERE ca.assigned_node IS NULL
        ORDER BY RANDOM()
        LIMIT p_limit
        FOR UPDATE SKIP LOCKED
    )
    UPDATE channel_assignments ca
    SET
        assigned_node = p_node_id,
        status = 'claimed',
        assigned_at = NOW(),
        updated_at = NOW()
    FROM picked
    WHERE ca.username = picked.username
      AND ca.site = picked.site
    RETURNING ca.*;
END;
$$;

-- Also fix claim_specific_channel (minor — no ORDER BY needed but keep consistent)

CREATE OR REPLACE FUNCTION claim_specific_channel(p_username TEXT, p_site TEXT, p_node_id TEXT)
RETURNS SETOF channel_assignments
LANGUAGE plpgsql
AS $$
DECLARE
    picked channel_assignments%ROWTYPE;
BEGIN
    SELECT ca.* INTO picked
    FROM channel_assignments ca
    WHERE ca.username = p_username
      AND ca.site = p_site
      AND ca.assigned_node IS NULL
    LIMIT 1
    FOR UPDATE SKIP LOCKED;

    IF FOUND THEN
        UPDATE channel_assignments ca
        SET
            assigned_node = p_node_id,
            status = 'claimed',
            assigned_at = NOW(),
            updated_at = NOW()
        WHERE ca.username = p_username
          AND ca.site = p_site
        RETURNING ca.* INTO picked;

        RETURN NEXT picked;
    END IF;

    RETURN;
END;
$$;

-- Fix reassign_channel for consistency (add updated_at)

CREATE OR REPLACE FUNCTION reassign_channel(p_username TEXT, p_site TEXT, p_from_node TEXT, p_to_node TEXT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE channel_assignments ca
    SET
        assigned_node = p_to_node,
        status = 'claimed',
        assigned_at = NOW(),
        updated_at = NOW()
    WHERE ca.username = p_username
      AND ca.site = p_site
      AND ca.assigned_node = p_from_node
      AND (
        ca.status = 'claimed'
        OR ca.status = 'offline'
        OR ca.status = 'unassigned'
      );
END;
$$;
