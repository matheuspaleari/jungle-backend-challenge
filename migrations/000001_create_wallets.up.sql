CREATE TABLE wallets (
    id UUID PRIMARY KEY,

    player_id TEXT NOT NULL,
    currency CHAR(3) NOT NULL,

    balance_minor BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT wallets_player_id_not_empty
        CHECK (btrim(player_id) <> ''),

    CONSTRAINT wallets_currency_format
        CHECK (currency ~ '^[A-Z]{3}$'),

    CONSTRAINT wallets_balance_non_negative
        CHECK (balance_minor >= 0),

    CONSTRAINT wallets_version_positive
        CHECK (version >= 1),

    CONSTRAINT wallets_updated_after_created
        CHECK (updated_at >= created_at),

    CONSTRAINT wallets_player_currency_unique
        UNIQUE (player_id, currency)
);