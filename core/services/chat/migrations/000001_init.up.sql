-- Chat schema. Applied as chat_service, whose search_path is pinned to `chat`,
-- so nothing here needs to qualify a name.
--
-- No users table, and no foreign key to one. Accounts live in the auth schema
-- and chat_service cannot read it — that is what the per-schema login role is
-- for. User ids are carried as plain uuids, vouched for by a verified access
-- token rather than by a constraint.

-- ---------------------------------------------------------------------------
-- rooms
-- ---------------------------------------------------------------------------
-- next_seq is the room's message counter, and it is here rather than in a
-- sequence on purpose. See the note on messages.seq below: a shared sequence
-- hands out numbers in an order that transactions are free to commit out of,
-- and a client paging by "everything after N" would silently skip whatever
-- committed late. Bumping a column on the room row takes a row lock, so within
-- one room the order numbers are assigned in is the order they commit in.
CREATE TABLE rooms (
    id         uuid        PRIMARY KEY,
    name       text        NOT NULL,
    created_by uuid        NOT NULL,
    next_seq   bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT rooms_name_present CHECK (length(btrim(name)) > 0),
    CONSTRAINT rooms_name_bounded CHECK (length(name) <= 120)
);

-- ---------------------------------------------------------------------------
-- room_members
-- ---------------------------------------------------------------------------
-- Membership is the authorization fact this service owns: whether somebody may
-- read a room or write to it is decided here, not at the gateway, which knows
-- who the caller is but not what they belong to.
--
-- display_name is a snapshot taken when the person joined, using their own
-- access token against auth. It is denormalised deliberately: the alternative
-- is a call to another service for every rendered message, which makes reading
-- history depend on auth being up.
CREATE TABLE room_members (
    room_id      uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL,
    display_name text        NOT NULL DEFAULT '',
    joined_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, user_id)
);

-- "Which rooms am I in" is the first call any client makes.
CREATE INDEX room_members_user_id_idx ON room_members (user_id);

-- ---------------------------------------------------------------------------
-- messages
-- ---------------------------------------------------------------------------
-- Postgres is the source of truth. Redis fans a message out to the other
-- instances and Kafka announces it to other domains, but neither is asked what
-- was said: a client that reconnects catches up from here.
--
-- seq is the cursor. Timestamps cannot be one — two messages can share an
-- instant, and paging by them either repeats a message or drops it. It is
-- allocated from rooms.next_seq, so it is per-room, monotonic, and assigned
-- under the room's row lock.
--
-- client_id is the id the sender made up before sending. It makes a resend
-- after a dropped connection idempotent: the unique index below turns the
-- second copy into a conflict the writer resolves by returning the first.
-- Empty means the sender offered none, and those must not collide with each
-- other, hence the partial index.
CREATE TABLE messages (
    id        uuid        PRIMARY KEY,
    room_id   uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    author_id uuid        NOT NULL,
    body      text        NOT NULL,
    client_id text        NOT NULL DEFAULT '',
    seq       bigint      NOT NULL,
    sent_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT messages_body_present CHECK (length(btrim(body)) > 0),
    CONSTRAINT messages_body_bounded CHECK (length(body) <= 4096)
);

-- The cursor index: every read of a room is "the page before seq" or "whatever
-- came after seq", and both are this index walked in one direction or the
-- other. Unique because two messages in one room sharing a position would make
-- paging ambiguous.
CREATE UNIQUE INDEX messages_room_seq_key ON messages (room_id, seq);

CREATE UNIQUE INDEX messages_client_id_key
    ON messages (room_id, author_id, client_id)
    WHERE client_id <> '';
