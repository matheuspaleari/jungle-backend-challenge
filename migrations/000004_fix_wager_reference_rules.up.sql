/*
 * Corrige as regras de referência das transações.
 *
 * REFUND / ROLLBACK:
 *   referência externa obrigatória.
 *
 * WIN:
 *   referência externa opcional.
 *
 * BET / LOSS / OPENING:
 *   não possuem referência externa.
 */

ALTER TABLE wager_transactions
DROP CONSTRAINT wager_transactions_reference_by_kind;


ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_reference_by_kind
CHECK (
    /*
     * REFUND e ROLLBACK obrigatoriamente
     * possuem referência.
     */
    (
        kind IN (
            'REFUND',
            'ROLLBACK'
        )
        AND reference_external_transaction_id IS NOT NULL
        AND btrim(reference_external_transaction_id) <> ''
    )

    OR

    /*
     * WIN pode possuir referência ou não.
     *
     * Se possuir, não pode ser uma string vazia.
     */
    (
        kind = 'WIN'
        AND (
            reference_external_transaction_id IS NULL
            OR btrim(reference_external_transaction_id) <> ''
        )
    )

    OR

    /*
     * OPENING, BET e LOSS não podem possuir
     * referência externa.
     */
    (
        kind IN (
            'OPENING',
            'BET',
            'LOSS'
        )
        AND reference_external_transaction_id IS NULL
    )
);


/*
 * resolved_reference_transaction_id representa
 * uma referência já resolvida internamente.
 *
 * Somente WIN, REFUND e ROLLBACK podem possuir
 * uma referência resolvida.
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_resolved_reference_by_kind
CHECK (
    resolved_reference_transaction_id IS NULL
    OR kind IN (
        'WIN',
        'REFUND',
        'ROLLBACK'
    )
);


/*
 * Uma transação nunca pode referenciar ela mesma.
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_no_self_reference
CHECK (
    resolved_reference_transaction_id IS NULL
    OR resolved_reference_transaction_id <> id
);