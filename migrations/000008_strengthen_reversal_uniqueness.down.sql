DROP INDEX IF EXISTS wager_transactions_successful_reversal_unique;

CREATE UNIQUE INDEX wager_transactions_successful_reversal_unique
    ON wager_transactions (resolved_reference_transaction_id, kind)
    WHERE state = 'PROCESSED'
      AND kind IN ('REFUND', 'ROLLBACK');
