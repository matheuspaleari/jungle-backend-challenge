-- #14 - Complete provider-scoped wallet identity.
--
-- Migration 000010 introduced the intended provider-scoped uniqueness:
--   (provider_id, player_id, currency)
--
-- The original schema-level UNIQUE(player_id, currency) still prevented two
-- independent providers from using the same external player identifier in the
-- same currency. Provider isolation requires those identities to be independent.
--
-- Legacy wallets with provider_id IS NULL continue to be preserved. This
-- migration only removes the obsolete cross-provider uniqueness constraint.

ALTER TABLE wallets
    DROP CONSTRAINT IF EXISTS wallets_player_currency_unique;
