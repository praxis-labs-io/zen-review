-- Where the comment sat when it was written, which is the range anchor_blob is
-- sliced by. start_line has moved on and cannot answer it. No backfill: a 0 on
-- an older row means the range was never recorded, and guessing it from the
-- anchor would be right only for a comment that never moved.

ALTER TABLE comments ADD COLUMN created_start_line INTEGER NOT NULL DEFAULT 0;

ALTER TABLE comments ADD COLUMN created_end_line INTEGER NOT NULL DEFAULT 0;
