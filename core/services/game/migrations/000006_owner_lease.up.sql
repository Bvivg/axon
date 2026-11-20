ALTER TABLE game.owners ADD COLUMN lease_until timestamptz NOT NULL DEFAULT clock_timestamp();
