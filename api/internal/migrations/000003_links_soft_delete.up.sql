-- Owner deletion becomes a soft delete (D31). The row stays, so the code
-- stays reserved: printed links and QR codes can't be taken over by whoever
-- registers the code next, and deleted links remain available for abuse
-- review. Deleted rows are invisible to redirects, listings and owner edits.
ALTER TABLE links ADD COLUMN deleted_at TIMESTAMPTZ;
