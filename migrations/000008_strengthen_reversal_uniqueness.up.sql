/*
 * Policy: a processed transaction may have at most one successful direct
 * reversal, regardless of whether that reversal is REFUND or ROLLBACK.
 *
 * This makes the rule safe at the database level across multiple application
 * instances. A rollback of a REFUND remains possible by referencing the REFUND
 * transaction itself, not the original BET.
 */
DROP INDEX IF EXISTS wager_transactions_successful_reversal_unique;

CREATE UNIQUE INDEX wager_transactions_successful_reversal_unique
    ON wager_transactions (resolved_reference_transaction_id)
    WHERE state = 'PROCESSED'
      AND kind IN ('REFUND', 'ROLLBACK');
