CREATE TABLE rooms (
    id         uuid        PRIMARY KEY,
    name       text        NOT NULL,
    created_by uuid        NOT NULL,
    next_seq   bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT rooms_name_present CHECK (length(btrim(name)) > 0),
    CONSTRAINT rooms_name_bounded CHECK (length(name) <= 120)
);

CREATE TABLE room_members (
    room_id      uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL,
    display_name text        NOT NULL DEFAULT '',
    joined_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX room_members_user_id_idx ON room_members (user_id);

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

CREATE UNIQUE INDEX messages_room_seq_key ON messages (room_id, seq);

CREATE UNIQUE INDEX messages_client_id_key
    ON messages (room_id, author_id, client_id)
    WHERE client_id <> '';
