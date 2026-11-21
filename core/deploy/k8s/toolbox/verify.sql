\set results `cat /tmp/load/results.json`
WITH r AS (
    SELECT coalesce(x->>'session', '') AS session,
           coalesce((x->>'moves')::int, 0) AS moves,
           coalesce(x->>'error', '') AS error
    FROM json_array_elements(:'results'::json) AS x
),
db AS (
    SELECT s.id::text AS session,
           coalesce(s.state->>'status', '') AS status,
           (SELECT count(*) FROM game.moves m WHERE m.session_id = s.id) AS moves
    FROM game.sessions s
    WHERE s.id::text IN (SELECT session FROM r WHERE session <> '')
)
SELECT json_build_object(
    'results', (SELECT count(*) FROM r),
    'completed_by_client', (SELECT count(*) FROM r WHERE error = '' AND session <> ''),
    'completed_and_matching_db', (SELECT count(*) FROM r JOIN db USING (session) WHERE r.error = '' AND db.moves = r.moves AND db.status IN ('win', 'draw')),
    'sessions_with_acknowledged_moves_missing', (SELECT count(*) FROM r JOIN db USING (session) WHERE db.moves < r.moves),
    'sessions_with_moves_missing_entirely', (SELECT count(*) FROM r LEFT JOIN db USING (session) WHERE r.session <> '' AND r.moves > 0 AND db.session IS NULL),
    'client_failures', (SELECT coalesce(json_object_agg(kind, n), '{}'::json) FROM (SELECT split_part(error, ':', 1) AS kind, count(*) AS n FROM r WHERE error <> '' GROUP BY 1) f)
);
