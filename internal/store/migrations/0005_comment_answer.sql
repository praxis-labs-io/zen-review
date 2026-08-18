-- The words an address leaves behind. A column rather than a second row, so it
-- freezes with the state it belongs to.
ALTER TABLE comments ADD COLUMN answer TEXT NOT NULL DEFAULT '';
