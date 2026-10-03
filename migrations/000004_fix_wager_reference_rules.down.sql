/*
 * Remove as novas proteções.
 */

ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS wager_transactions_no_self_reference;


ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS wager_transactions_resolved_reference_by_kind;


/*
 * Restaura a regra existente antes da migration 000004:
 *
 * somente REFUND e ROLLBACK podem possuir
 * reference_external_transaction_id.
 */

ALTER TABLE wager_transactions
DROP CONSTRAINT IF EXISTS wager_transactions_reference_by_kind;


ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_reference_by_kind
CHECK (
    (
        kind IN (
            'REFUND',
            'ROLLBACK'
        )
        AND reference_external_transaction_id IS NOT NULL
        AND btrim(reference_external_transaction_id) <> ''
    )
    OR
    (
        kind NOT IN (
            'REFUND',
            'ROLLBACK'
        )
        AND reference_external_transaction_id IS NULL
    )
);