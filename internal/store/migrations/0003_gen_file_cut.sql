-- gen_files.cut is the refresh saying it took reviewed lines off this file.
--
-- A range that failed to translate and a range somebody withdrew leave the same
-- coverage behind, so nothing reading the coverage can tell them apart. Only the
-- refresh knows, because only the refresh ran the translation, and this is where
-- it writes that down.
--
-- 0002 rebuilt this table because SQLite cannot add a CHECK to an existing
-- column. It takes one on a new column, so this is an ordinary ALTER, and every
-- row already written predates the record and gets the 0 the default gives it.
--
-- The CHECK is what every other closed vocabulary in this schema has. Without it
-- a stray integer is a scan into a bool that fails on read, which is a long way
-- from the write that put it there.

ALTER TABLE gen_files ADD COLUMN cut INTEGER NOT NULL DEFAULT 0 CHECK (cut IN (0, 1));
