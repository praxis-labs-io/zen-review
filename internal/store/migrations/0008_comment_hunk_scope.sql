-- scope gains 'hunk'. SQLite cannot widen a CHECK in place, so the table is rebuilt and every row copied across.

CREATE TABLE comments_rebuilt (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,

    generation_id         INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    created_generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,

    path                  TEXT NOT NULL,
    side                  TEXT NOT NULL CHECK (side IN ('head', 'base')),

    start_line            INTEGER NOT NULL DEFAULT 0,
    end_line              INTEGER NOT NULL DEFAULT 0,

    scope                 TEXT NOT NULL CHECK (scope IN ('line', 'range', 'hunk', 'file')),
    body                  TEXT NOT NULL,
    response              TEXT NOT NULL DEFAULT '',

    state                 TEXT NOT NULL CHECK (state IN ('open', 'addressed', 'resolved', 'orphaned')),

    anchor_blob           TEXT NOT NULL DEFAULT '',
    last_path             TEXT NOT NULL DEFAULT '',
    last_line             INTEGER NOT NULL DEFAULT 0,

    created_start_line    INTEGER NOT NULL DEFAULT 0,
    created_end_line      INTEGER NOT NULL DEFAULT 0,

    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,

    CHECK (scope = 'file' OR (start_line > 0 AND end_line >= start_line)),
    CHECK (scope <> 'file' OR (start_line = 0 AND end_line = 0)),
    CHECK (scope <> 'line' OR start_line = end_line)
);

INSERT INTO comments_rebuilt (
    id, session_id, generation_id, created_generation_id, path, side, start_line, end_line,
    scope, body, response, state, anchor_blob, last_path, last_line,
    created_start_line, created_end_line, created_at, updated_at)
SELECT
    id, session_id, generation_id, created_generation_id, path, side, start_line, end_line,
    scope, body, response, state, anchor_blob, last_path, last_line,
    created_start_line, created_end_line, created_at, updated_at
FROM comments;

DROP TABLE comments;

ALTER TABLE comments_rebuilt RENAME TO comments;

CREATE INDEX comments_by_state ON comments (session_id, state);

CREATE INDEX comments_by_generation ON comments (generation_id);
