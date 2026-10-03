ALTER TABLE outbox_events
    ADD COLUMN locked_by TEXT,
    ADD COLUMN locked_until TIMESTAMPTZ,
    ADD COLUMN last_error TEXT;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_lock_pair
        CHECK (
            (locked_by IS NULL AND locked_until IS NULL)
            OR
            (locked_by IS NOT NULL AND btrim(locked_by) <> '' AND locked_until IS NOT NULL)
        );

CREATE INDEX outbox_events_claim_idx
    ON outbox_events (next_attempt_at, locked_until, occurred_at, id)
    WHERE published_at IS NULL;
