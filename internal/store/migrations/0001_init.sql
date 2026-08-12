-- The review database. Five tables and no more: hunks are derived from the diff
-- at render time and nothing about them persists, which is what keeps this
-- small.
--
-- Every column is NOT NULL. A value that may be absent defaults to '' or 0
-- rather than NULL, so no row type carries a null wrapper and no read has a
-- branch for a column that happened to be empty.
--
-- Timestamps are RFC3339 in UTC, so sqlite3 on the file reads without a decoder.

-- A session is one repository plus one thing to review in it, resumable days
-- later.
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,

    -- The resolved git common dir, so a linked worktree and the checkout it came
    -- from agree about which repository they are in.
    repo_path  TEXT NOT NULL,

    kind       TEXT NOT NULL CHECK (kind IN ('branch', 'range', 'detached')),
    branch     TEXT NOT NULL DEFAULT '',

    -- The key that is not a branch: an explicit range, or the sha a detached
    -- HEAD keys on.
    range_spec TEXT NOT NULL DEFAULT '',

    -- Never re-detected. A saved session keeps the base it has until the reader
    -- names another one, and re-detecting it on every run is how a base moves
    -- under a half-finished review.
    base_ref   TEXT NOT NULL DEFAULT '',

    summary    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- A generation is one snapshot of the changeset, written into git as a real
-- commit. commit_sha is that commit, and its tree is what every blob sha below
-- resolves against.
CREATE TABLE generations (
    id         INTEGER PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,

    -- The merge base the changeset was measured from, and the branch tip at the
    -- time.
    base_sha   TEXT NOT NULL,
    head_sha   TEXT NOT NULL,

    commit_sha TEXT NOT NULL,
    created_at TEXT NOT NULL,

    UNIQUE (session_id, seq)
);

-- One row per file in the changeset at one generation. The blob shas here are
-- real objects because the generation commit wrote them, which is what
-- remapping diffs through.
CREATE TABLE gen_files (
    generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,

    -- Set on a rename or a copy.
    old_path      TEXT NOT NULL DEFAULT '',

    -- Mirrors diff.Status. It carries no CHECK, unlike every other column here
    -- with a fixed vocabulary, because a generation tree records an embedded
    -- repository as a gitlink and that case is still being settled. Adding the
    -- constraint once it is costs one migration; guessing it now costs two.
    status        TEXT NOT NULL,

    -- Empty on the side the file does not exist on.
    base_blob     TEXT NOT NULL DEFAULT '',
    head_blob     TEXT NOT NULL DEFAULT '',

    PRIMARY KEY (generation_id, path)
);

-- Reviewed state is line ranges, never hunk indices: an agent inserting twenty
-- lines above a hunk leaves different code wearing the same label. A range that
-- fails to translate from one generation to the next disappears, and that is
-- what makes a hunk read as changed after review.
--
-- side is 'head' except for a deletion-only hunk, which has no head-side lines
-- and anchors to the base blob instead.
--
-- There is no UNIQUE constraint, so a write normalises the whole of one file's
-- ranges inside its own transaction rather than leaning on one.
CREATE TABLE reviewed_ranges (
    session_id    TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    side          TEXT NOT NULL CHECK (side IN ('head', 'base')),
    start_line    INTEGER NOT NULL,
    end_line      INTEGER NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE INDEX reviewed_ranges_by_file ON reviewed_ranges (generation_id, path);

-- A comment anchors to a generation, a file, a side and a line range.
--
-- Nothing writes this table yet either.
CREATE TABLE comments (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    side          TEXT NOT NULL CHECK (side IN ('head', 'base')),
    start_line    INTEGER NOT NULL,
    end_line      INTEGER NOT NULL,
    scope         TEXT NOT NULL CHECK (scope IN ('line', 'range', 'file', 'session')),
    body          TEXT NOT NULL,

    -- An agent reaches 'addressed' and never 'resolved'. The claim and the
    -- confirmation are different facts and the queue shows them as such.
    state         TEXT NOT NULL CHECK (state IN ('open', 'addressed', 'resolved', 'orphaned')),

    -- What an orphaned comment keeps. A rewrite that loses the anchor leaves the
    -- text and its last known location rather than swallowing what was said.
    anchor_blob   TEXT NOT NULL DEFAULT '',
    last_path     TEXT NOT NULL DEFAULT '',
    last_line     INTEGER NOT NULL DEFAULT 0,

    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX comments_by_state ON comments (session_id, state);
