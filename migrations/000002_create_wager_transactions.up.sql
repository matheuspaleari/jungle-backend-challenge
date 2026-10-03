CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY,

    external_transaction_id TEXT,
    provider_id TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id),

    player_id TEXT NOT NULL,
    round_id TEXT,
    game_id TEXT,

    kind TEXT NOT NULL,

    amount_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,

    reference_external_transaction_id TEXT,

    resolved_reference_transaction_id UUID
        REFERENCES wager_transactions(id),

    state TEXT NOT NULL,

    failure_code TEXT,

    -- Saldo observado após a operação.
    -- Será utilizado posteriormente para garantir que um replay
    -- devolva exatamente o resultado persistido da operação original.
    result_balance_minor BIGINT,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT wager_transactions_player_not_empty
        CHECK (
            btrim(player_id) <> ''
        ),

    CONSTRAINT wager_transactions_currency_format
        CHECK (
            currency ~ '^[A-Z]{3}$'
        ),

    CONSTRAINT wager_transactions_amount_non_negative
        CHECK (
            amount_minor >= 0
        ),

    CONSTRAINT wager_transactions_result_balance_non_negative
        CHECK (
            result_balance_minor IS NULL
            OR result_balance_minor >= 0
        ),

    CONSTRAINT wager_transactions_kind_valid
        CHECK (
            kind IN (
                'OPENING',
                'BET',
                'WIN',
                'LOSS',
                'REFUND',
                'ROLLBACK'
            )
        ),

    CONSTRAINT wager_transactions_state_valid
        CHECK (
            state IN (
                'PENDING',
                'PENDING_REFERENCE',
                'PROCESSED',
                'REJECTED',
                'FAILED'
            )
        ),

    /*
     * Regras de valor:
     *
     * LOSS:
     *   sempre exatamente zero.
     *
     * OPENING:
     *   pode ser zero ou positivo.
     *
     * BET/WIN/REFUND/ROLLBACK:
     *   sempre maior que zero.
     */
    CONSTRAINT wager_transactions_amount_by_kind
        CHECK (
            (
                kind = 'LOSS'
                AND amount_minor = 0
            )
            OR
            (
                kind = 'OPENING'
                AND amount_minor >= 0
            )
            OR
            (
                kind IN (
                    'BET',
                    'WIN',
                    'REFUND',
                    'ROLLBACK'
                )
                AND amount_minor > 0
            )
        ),

    /*
     * REFUND e ROLLBACK precisam obrigatoriamente
     * informar a transação externa de referência.
     *
     * Os demais tipos não podem possuir essa referência.
     */
    CONSTRAINT wager_transactions_reference_by_kind
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
        ),

    /*
     * OPENING é uma operação exclusivamente interna.
     *
     * Portanto não possui:
     * - external_transaction_id
     * - provider_id
     * - idempotency_key
     * - payload_hash
     * - round_id
     * - game_id
     * - reference_external_transaction_id
     *
     * Todas as demais operações são externas e precisam
     * possuir os identificadores correspondentes.
     */
    CONSTRAINT wager_transactions_opening_fields
        CHECK (
            (
                kind = 'OPENING'

                AND external_transaction_id IS NULL
                AND provider_id IS NULL
                AND idempotency_key IS NULL
                AND payload_hash IS NULL

                AND round_id IS NULL
                AND game_id IS NULL

                AND reference_external_transaction_id IS NULL
            )
            OR
            (
                kind <> 'OPENING'

                AND external_transaction_id IS NOT NULL
                AND btrim(external_transaction_id) <> ''

                AND provider_id IS NOT NULL
                AND btrim(provider_id) <> ''

                AND idempotency_key IS NOT NULL
                AND btrim(idempotency_key) <> ''

                AND payload_hash IS NOT NULL
                AND btrim(payload_hash) <> ''

                AND round_id IS NOT NULL
                AND btrim(round_id) <> ''

                AND game_id IS NOT NULL
                AND btrim(game_id) <> ''
            )
        ),

    /*
     * REJECTED e FAILED precisam possuir um código
     * persistido explicando a falha.
     *
     * Estados não relacionados a falha não possuem
     * failure_code.
     */
    CONSTRAINT wager_transactions_failure_code_state
        CHECK (
            (
                state IN (
                    'REJECTED',
                    'FAILED'
                )

                AND failure_code IS NOT NULL
                AND btrim(failure_code) <> ''
            )
            OR
            (
                state NOT IN (
                    'REJECTED',
                    'FAILED'
                )

                AND failure_code IS NULL
            )
        ),

    CONSTRAINT wager_transactions_updated_after_created
        CHECK (
            updated_at >= created_at
        )
);


/*
 * IDEMPOTÊNCIA
 *
 * Um provider não pode reutilizar a mesma idempotency key
 * para criar outra transação.
 */
CREATE UNIQUE INDEX wager_transactions_provider_idempotency_unique
    ON wager_transactions (
        provider_id,
        idempotency_key
    )
    WHERE provider_id IS NOT NULL;


/*
 * IDENTIDADE DA TRANSAÇÃO EXTERNA
 *
 * Mesmo usando outra idempotency key, o provider não pode
 * aplicar novamente a mesma externalTransactionId.
 */
CREATE UNIQUE INDEX wager_transactions_provider_external_unique
    ON wager_transactions (
        provider_id,
        external_transaction_id
    )
    WHERE provider_id IS NOT NULL;


/*
 * OPENING
 *
 * Cada wallet pode possuir no máximo uma transação
 * interna de abertura.
 */
CREATE UNIQUE INDEX wager_transactions_opening_wallet_unique
    ON wager_transactions (
        wallet_id
    )
    WHERE kind = 'OPENING';


/*
 * REVERSÕES
 *
 * Impede duas reversões processadas do mesmo tipo
 * contra a mesma transação original.
 *
 * Exemplo:
 *
 * BET X
 *   └── REFUND #1 -> permitido
 *   └── REFUND #2 -> bloqueado
 */
CREATE UNIQUE INDEX wager_transactions_successful_reversal_unique
    ON wager_transactions (
        resolved_reference_transaction_id,
        kind
    )
    WHERE state = 'PROCESSED'
      AND kind IN (
          'REFUND',
          'ROLLBACK'
      );


/*
 * Consultas de histórico por wallet.
 */
CREATE INDEX wager_transactions_wallet_created_idx
    ON wager_transactions (
        wallet_id,
        created_at DESC
    );


/*
 * Worker responsável por tentar resolver referências
 * que ainda não chegaram.
 *
 * Mais tarde adicionaremos informações de retry/backoff
 * necessárias para o worker.
 */
CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (
        created_at
    )
    WHERE state = 'PENDING_REFERENCE';