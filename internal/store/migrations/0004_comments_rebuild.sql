-- The comments table gets the shape a comment can survive a remap in.
--
-- generation_id moves forward every time a refresh translates the anchor, so it
-- cannot answer where the comment started. created_generation_id can, and that
-- is the generation whose tree anchor_blob resolves against.
--
-- scope loses 'session'. sessions.summary is already there and already read, and
-- two ways to say the same thing is one too many.
--
-- SQLite cannot narrow a CHECK or add a table-level one in place, so the table is
-- rebuilt the way 0002 rebuilt gen_files. Nothing has ever written a row, so
-- nothing is copied across, and created_generation_id has no value an old row
-- could have supplied.

CREATE TABLE comments_rebuilt (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,

    -- Where the comment sits now, moved forward by every refresh that translates
    -- its anchor, and where it started.
    generation_id         INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    created_generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,

    path                  TEXT NOT NULL,
    side                  TEXT NOT NULL CHECK (side IN ('head', 'base')),

    -- 0 and 0 for a file comment, which names the file rather than any line in
    -- it, the way a whole-file reviewed range does.
    start_line            INTEGER NOT NULL DEFAULT 0,
    end_line              INTEGER NOT NULL DEFAULT 0,

    scope                 TEXT NOT NULL CHECK (scope IN ('line', 'range', 'file')),
    body                  TEXT NOT NULL,

    -- An agent reaches 'addressed' and never 'resolved'. The claim and the
    -- confirmation are different facts and the queue shows them as such.
    --
    -- Only an open comment orphans. One already addressed or resolved that loses
    -- its anchor was acted on, which is the rewrite that destroyed it.
    state                 TEXT NOT NULL CHECK (state IN ('open', 'addressed', 'resolved', 'orphaned')),

    -- What a comment that has stopped moving keeps: the exact bytes it was about,
    -- immune to every rename and held alive by the session ref, and the last place
    -- the anchor was. An addressed comment writes these too, not only an orphaned
    -- one.
    anchor_blob           TEXT NOT NULL DEFAULT '',
    last_path             TEXT NOT NULL DEFAULT '',
    last_line             INTEGER NOT NULL DEFAULT 0,

    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,

    -- A scope is a claim about what the comment is on, and the lines are how it
    -- is kept. A file comment carrying lines and a range comment carrying none
    -- are both a scope saying one thing while the row says another.
    --
    -- Neither names a scope the column above has not already listed. One that
    -- did would be the vocabulary written twice, and the CHECK on scope would
    -- stop catching anything of its own.
    CHECK (scope = 'file' OR (start_line > 0 AND end_line >= start_line)),
    CHECK (scope <> 'file' OR (start_line = 0 AND end_line = 0))
);

DROP TABLE comments;

ALTER TABLE comments_rebuilt RENAME TO comments;

-- Dropping a table drops its indexes, so the state one is made again rather than
-- carried. The generation one is what a refresh reads by: every comment at the
-- latest generation, to translate its anchor onto the new one.
CREATE INDEX comments_by_state ON comments (session_id, state);

CREATE INDEX comments_by_generation ON comments (generation_id);
