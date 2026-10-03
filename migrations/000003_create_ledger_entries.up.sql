CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id),

    transaction_id UUID NOT NULL
        REFERENCES wager_transactions(id),

    direction TEXT NOT NULL,

    value_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,

    balance_before_minor BIGINT NOT NULL,
    balance_after_minor BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT ledger_entries_direction_valid
        CHECK (
            direction IN (
                'DEBIT',
                'CREDIT'
            )
        ),

    CONSTRAINT ledger_entries_value_positive
        CHECK (
            value_minor > 0
        ),

    CONSTRAINT ledger_entries_currency_format
        CHECK (
            currency ~ '^[A-Z]{3}$'
        ),

    CONSTRAINT ledger_entries_balance_before_non_negative
        CHECK (
            balance_before_minor >= 0
        ),

    CONSTRAINT ledger_entries_balance_after_non_negative
        CHECK (
            balance_after_minor >= 0
        ),

    /*
     * O saldo resultante precisa corresponder exatamente
     * ao movimento financeiro registrado.
     *
     * DEBIT:
     * balanceAfter = balanceBefore - value
     *
     * CREDIT:
     * balanceAfter = balanceBefore + value
     */
    CONSTRAINT ledger_entries_balance_consistency
        CHECK (
            (
                direction = 'DEBIT'
                AND balance_after_minor =
                    balance_before_minor - value_minor
            )
            OR
            (
                direction = 'CREDIT'
                AND balance_after_minor =
                    balance_before_minor + value_minor
            )
        ),

    /*
     * Uma transacao financeira pode gerar no maximo
     * um movimento por wallet.
     */
    CONSTRAINT ledger_entries_wallet_transaction_unique
        UNIQUE (
            wallet_id,
            transaction_id
        )
);


/*
 * Historico da wallet.
 *
 * Esse indice sera utilizado pelo endpoint:
 *
 * GET /wallets/{walletId}/ledger
 */
CREATE INDEX ledger_entries_wallet_created_idx
    ON ledger_entries (
        wallet_id,
        created_at DESC,
        id DESC
    );


/*
 * Protecao append-only.
 *
 * Ledger financeiro nunca pode ser alterado ou removido.
 *
 * Correcao financeira deve acontecer atraves de um novo
 * movimento (REFUND/ROLLBACK), nunca modificando historia.
 */
CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION
        'ledger entries are immutable: % is not allowed',
        TG_OP;
END;
$$;


/*
 * Qualquer UPDATE sera rejeitado.
 */
CREATE TRIGGER ledger_entries_prevent_update
BEFORE UPDATE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_mutation();


/*
 * Qualquer DELETE sera rejeitado.
 */
CREATE TRIGGER ledger_entries_prevent_delete
BEFORE DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_mutation();