CREATE TABLE game.moves_unpartitioned (
    session_id uuid NOT NULL REFERENCES game.sessions (id),
    ply integer NOT NULL CHECK (ply > 0),
    player_id uuid NOT NULL,
    data jsonb NOT NULL,
    state_before jsonb,
    clock_before jsonb,
    PRIMARY KEY (session_id, ply)
);

INSERT INTO game.moves_unpartitioned (session_id, ply, player_id, data, state_before, clock_before)
SELECT DISTINCT ON (session_id, ply) session_id, ply, player_id, data, state_before, clock_before
FROM game.moves
ORDER BY session_id, ply, played_at DESC;

DROP TABLE game.moves;
DROP FUNCTION game.ensure_move_partitions(integer, timestamptz);

ALTER TABLE game.moves_unpartitioned RENAME TO moves;
ALTER TABLE game.moves RENAME CONSTRAINT moves_unpartitioned_pkey TO moves_pkey;
ALTER TABLE game.moves RENAME CONSTRAINT moves_unpartitioned_session_id_fkey TO moves_session_id_fkey;
ALTER TABLE game.moves RENAME CONSTRAINT moves_unpartitioned_ply_check TO moves_ply_check;
