-- v0 -> v1: Latest revision

CREATE TABLE portal (
    account_id BIGINT,
    chat_id    BIGINT,

    mxid       TEXT UNIQUE,
    type       INT NOT NULL,
    plain_name TEXT NOT NULL,
    name       TEXT NOT NULL,
    name_set   BOOLEAN NOT NULL,
    topic      TEXT NOT NULL,
    topic_set  BOOLEAN NOT NULL,
    avatar     TEXT NOT NULL,
    avatar_url TEXT NOT NULL,
    avatar_set BOOLEAN NOT NULL,
    encrypted  BOOLEAN NOT NULL,

    PRIMARY KEY (account_id, chat_id)
);

CREATE TABLE puppet (
    account_id BIGINT NOT NULL,
    contact_id BIGINT NOT NULL,

    name       TEXT NOT NULL,
    name_set   BOOLEAN NOT NULL,
    avatar     TEXT NOT NULL,
    avatar_url TEXT NOT NULL,
    avatar_set BOOLEAN NOT NULL,

    custom_mxid  TEXT,
    access_token TEXT,
    next_batch   TEXT,

    PRIMARY KEY (account_id, contact_id)
);

CREATE TABLE "user" (
    mxid            TEXT PRIMARY KEY,
    account_id      BIGINT UNIQUE NULL,
    management_room TEXT NOT NULL
);
