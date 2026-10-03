DROP INDEX IF EXISTS outbox_events_claim_idx;

ALTER TABLE outbox_events
    DROP CONSTRAINT IF EXISTS outbox_events_lock_pair,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS locked_by;
