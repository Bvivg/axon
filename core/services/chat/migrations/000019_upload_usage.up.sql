CREATE TABLE upload_usage (
    user_id uuid   PRIMARY KEY,
    day     date   NOT NULL,
    bytes   bigint NOT NULL CHECK (bytes >= 0)
);
