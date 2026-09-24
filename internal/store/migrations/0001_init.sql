-- SPDX-License-Identifier: Apache-2.0
-- Initial schema. Document tables keep the API object as JSON in `data` and
-- copy the fields they are queried or ordered by into columns.

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,
    created_at        INTEGER NOT NULL, -- unix nanoseconds
    data              TEXT NOT NULL,
    -- Hash of the per-session ingest token; the token itself is never stored.
    ingest_token_hash TEXT NOT NULL DEFAULT ''
);

-- Final captions for exports and replay; one row per (session, track, segment).
CREATE TABLE captions (
    session_id  TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    track       TEXT NOT NULL, -- `source` or a target language code
    segment_id  TEXT NOT NULL,
    start_sec   REAL NOT NULL,
    end_sec     REAL NOT NULL,
    text        TEXT NOT NULL,
    source_lang TEXT NOT NULL,
    final       INTEGER NOT NULL DEFAULT 1,
    edited      INTEGER NOT NULL DEFAULT 0,
    hidden      INTEGER NOT NULL DEFAULT 0,
    latency_ms  INTEGER,
    PRIMARY KEY (session_id, track, segment_id)
);
CREATE INDEX captions_by_start ON captions (session_id, start_sec, track, segment_id);

-- Single-row tables.
CREATE TABLE settings (
    id   INTEGER PRIMARY KEY CHECK (id = 1),
    data TEXT NOT NULL
);
CREATE TABLE admin (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    pin_hash TEXT NOT NULL
);

CREATE TABLE glossaries (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data TEXT NOT NULL
);

CREATE TABLE overlay_presets (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data TEXT NOT NULL
);

-- No foreign key to sessions: a recording (and its audio file) outlives a
-- deleted session until retention or an explicit delete removes it.
CREATE TABLE recordings (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    started_at INTEGER NOT NULL, -- unix nanoseconds
    data       TEXT NOT NULL
);
CREATE INDEX recordings_by_session ON recordings (session_id, started_at);
