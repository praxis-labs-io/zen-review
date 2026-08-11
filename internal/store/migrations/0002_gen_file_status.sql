-- gen_files.status gets the CHECK 0001 left off it.
--
-- What was unsettled then was the embedded repository. `git add -A` records one
-- as a mode 160000 gitlink, and the diff of that tree against the base reports
-- it as an ordinary added, modified or deleted file carrying a single
-- "Subproject commit" line. diff.Status gains no value, so the vocabulary is
-- closed and the column can say so.
--
-- SQLite cannot add a constraint in place, so the table is rebuilt. Nothing
-- references gen_files, so a create, a copy, a drop and a rename is the whole
-- of it.

CREATE TABLE gen_files_rebuilt (
    generation_id INTEGER NOT NULL REFERENCES generations (id) ON DELETE CASCADE,
    path          TEXT NOT NULL,

    -- Set on a rename or a copy.
    old_path      TEXT NOT NULL DEFAULT '',

    status        TEXT NOT NULL CHECK (status IN ('added', 'modified', 'deleted', 'renamed', 'copied')),

    -- Empty on the side the file does not exist on. A gitlink's head_blob is the
    -- embedded repository's commit sha rather than a blob, and that object is not
    -- in this object store. It is stored anyway, because it is the identity that
    -- changed, and a remap has to allow for it rather than assume every value
    -- here resolves.
    base_blob     TEXT NOT NULL DEFAULT '',
    head_blob     TEXT NOT NULL DEFAULT '',

    PRIMARY KEY (generation_id, path)
);

INSERT INTO gen_files_rebuilt (generation_id, path, old_path, status, base_blob, head_blob)
SELECT generation_id, path, old_path, status, base_blob, head_blob FROM gen_files;

DROP TABLE gen_files;

ALTER TABLE gen_files_rebuilt RENAME TO gen_files;
