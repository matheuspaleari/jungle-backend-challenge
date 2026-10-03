DROP INDEX IF EXISTS wallets_provider_player_currency_unique;
DROP INDEX IF EXISTS wallets_provider_id_idx;
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_provider_id_not_empty;
ALTER TABLE wallets DROP COLUMN IF EXISTS provider_id;
