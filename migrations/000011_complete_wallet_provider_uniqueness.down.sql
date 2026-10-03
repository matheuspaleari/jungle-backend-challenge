-- Rollback restores the legacy global uniqueness rule.
-- This can fail if the database already contains the same player/currency under
-- multiple providers, which is valid after the UP migration. Such data must be
-- reconciled before intentionally rolling this migration back.

ALTER TABLE wallets
    ADD CONSTRAINT wallets_player_currency_unique
    UNIQUE (player_id, currency);
