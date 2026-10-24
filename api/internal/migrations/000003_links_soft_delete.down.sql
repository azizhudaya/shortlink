-- Local resets only. Soft-deleted rows had no visible effect except keeping
-- their code reserved; going back to hard deletes removes them.
DELETE FROM links WHERE deleted_at IS NOT NULL;
ALTER TABLE links DROP COLUMN deleted_at;
