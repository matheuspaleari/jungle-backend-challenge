/*
 * Persistencia do mecanismo de resolucao de referencias.
 *
 * Operacoes como REFUND e ROLLBACK podem chegar antes da
 * transacao original.
 *
 * Nesses casos a WagerTransaction fica em
 * PENDING_REFERENCE e o worker precisa conseguir retomar
 * as tentativas mesmo depois de restart da aplicacao.
 */

ALTER TABLE wager_transactions
ADD COLUMN reference_attempt_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE wager_transactions
ADD COLUMN reference_next_attempt_at TIMESTAMPTZ;

ALTER TABLE wager_transactions
ADD COLUMN reference_expires_at TIMESTAMPTZ;


/*
 * Nunca podemos possuir quantidade negativa de tentativas.
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_reference_attempt_count_non_negative
CHECK (
    reference_attempt_count >= 0
);


/*
 * Datas de retry/expiracao somente fazem sentido enquanto
 * a transacao esta aguardando uma referencia.
 *
 * PENDING_REFERENCE:
 *   - next_attempt_at obrigatorio
 *   - expires_at obrigatorio
 *
 * Demais estados:
 *   - ambos devem ser NULL
 *
 * O contador pode permanecer persistido depois que a
 * transacao deixa PENDING_REFERENCE para fins de auditoria.
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_reference_schedule_by_state
CHECK (
    (
        state = 'PENDING_REFERENCE'
        AND reference_next_attempt_at IS NOT NULL
        AND reference_expires_at IS NOT NULL
    )
    OR
    (
        state <> 'PENDING_REFERENCE'
        AND reference_next_attempt_at IS NULL
        AND reference_expires_at IS NULL
    )
);


/*
 * A expiracao precisa acontecer depois da proxima
 * tentativa programada.
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_reference_expiration_valid
CHECK (
    reference_next_attempt_at IS NULL
    OR reference_expires_at IS NULL
    OR reference_expires_at >= reference_next_attempt_at
);


/*
 * Uma transacao em PENDING_REFERENCE precisa realmente
 * possuir uma referencia externa para ser resolvida.
 *
 * Isso inclui:
 * - REFUND
 * - ROLLBACK
 * - WIN quando ele possuir referencia opcional
 */
ALTER TABLE wager_transactions
ADD CONSTRAINT wager_transactions_pending_reference_has_reference
CHECK (
    state <> 'PENDING_REFERENCE'
    OR (
        reference_external_transaction_id IS NOT NULL
        AND btrim(reference_external_transaction_id) <> ''
    )
);


/*
 * Substitui o indice anterior.
 *
 * O worker nao precisa varrer todas as transacoes
 * PENDING_REFERENCE.
 *
 * Ele pode buscar somente aquelas cujo horario da
 * proxima tentativa ja chegou.
 */
DROP INDEX IF EXISTS wager_transactions_pending_reference_idx;

CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (
        reference_next_attempt_at,
        created_at,
        id
    )
    WHERE state = 'PENDING_REFERENCE';