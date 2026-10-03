DROP INDEX IF EXISTS wager_transactions_pending_reference_idx;


ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS
    wager_transactions_pending_reference_has_reference;

ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS
    wager_transactions_reference_expiration_valid;

ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS
    wager_transactions_reference_schedule_by_state;

ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS
    wager_transactions_reference_attempt_count_non_negative;


ALTER TABLE wager_transactions
DROP COLUMN IF EXISTS reference_expires_at;

ALTER TABLE wager_transactions
DROP COLUMN IF EXISTS reference_next_attempt_at;

ALTER TABLE wager_transactions
DROP COLUMN IF EXISTS reference_attempt_count;


/*
 * Restaura o indice existente antes desta migration.
 */
CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (
        created_at
    )
    WHERE state = 'PENDING_REFERENCE';