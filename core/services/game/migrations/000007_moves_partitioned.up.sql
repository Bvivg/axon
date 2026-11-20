ALTER TABLE game.moves RENAME TO moves_unpartitioned;
ALTER TABLE game.moves_unpartitioned RENAME CONSTRAINT moves_pkey TO moves_unpartitioned_pkey;
ALTER TABLE game.moves_unpartitioned RENAME CONSTRAINT moves_session_id_fkey TO moves_unpartitioned_session_id_fkey;
ALTER TABLE game.moves_unpartitioned RENAME CONSTRAINT moves_ply_check TO moves_unpartitioned_ply_check;

CREATE TABLE game.moves (
    session_id uuid NOT NULL REFERENCES game.sessions (id),
    ply integer NOT NULL CHECK (ply > 0),
    player_id uuid NOT NULL,
    data jsonb NOT NULL,
    state_before jsonb,
    clock_before jsonb,
    played_at timestamptz NOT NULL,
    PRIMARY KEY (session_id, ply, played_at)
) PARTITION BY RANGE (played_at);

CREATE TABLE game.moves_default PARTITION OF game.moves DEFAULT;

CREATE FUNCTION game.ensure_move_partitions(months_ahead integer, since timestamptz DEFAULT clock_timestamp())
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    month_start timestamp := date_trunc('month', since AT TIME ZONE 'UTC');
    last_month timestamp := date_trunc('month', clock_timestamp() AT TIME ZONE 'UTC') + make_interval(months => months_ahead);
    lower_bound text;
    upper_bound text;
    partition_name text;
    created integer := 0;
BEGIN
    IF months_ahead < 0 THEN
        RAISE EXCEPTION 'months_ahead must not be negative, got %', months_ahead;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtext('game.ensure_move_partitions'));
    WHILE month_start <= last_month LOOP
        partition_name := 'moves_' || to_char(month_start, 'YYYY_MM');
        lower_bound := to_char(month_start, 'YYYY-MM-DD') || ' 00:00:00+00';
        upper_bound := to_char(month_start + interval '1 month', 'YYYY-MM-DD') || ' 00:00:00+00';
        IF to_regclass(format('game.%I', partition_name)) IS NULL THEN
            EXECUTE format('CREATE TABLE game.%I (LIKE game.moves INCLUDING DEFAULTS INCLUDING CONSTRAINTS)', partition_name);
            EXECUTE format(
                'WITH adopted AS (DELETE FROM game.moves_default WHERE played_at >= %L::timestamptz AND played_at < %L::timestamptz RETURNING *) INSERT INTO game.%I SELECT * FROM adopted',
                lower_bound, upper_bound, partition_name);
            EXECUTE format('ALTER TABLE game.moves ATTACH PARTITION game.%I FOR VALUES FROM (%L) TO (%L)', partition_name, lower_bound, upper_bound);
            created := created + 1;
        END IF;
        month_start := month_start + interval '1 month';
    END LOOP;
    RETURN created;
END
$$;

ALTER TABLE game.moves_unpartitioned ADD COLUMN played_at timestamptz;

UPDATE game.moves_unpartitioned m
SET played_at = COALESCE((m.clock_before->>'started_at')::timestamptz, s.updated_at)
FROM game.sessions s
WHERE s.id = m.session_id;

SELECT game.ensure_move_partitions(3, LEAST(clock_timestamp(), GREATEST(
    COALESCE(min(played_at), clock_timestamp()),
    clock_timestamp() - interval '24 months'
)))
FROM game.moves_unpartitioned;

INSERT INTO game.moves (session_id, ply, player_id, data, state_before, clock_before, played_at)
SELECT session_id, ply, player_id, data, state_before, clock_before, played_at
FROM game.moves_unpartitioned;

DROP TABLE game.moves_unpartitioned;
