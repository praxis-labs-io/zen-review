-- answer implied a question, and half a queue is change requests where the
-- agent's words respond to one rather than answering anything.
ALTER TABLE comments RENAME COLUMN answer TO response;
