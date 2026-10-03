-- #10B - Persistent provider ownership for wallets.
--
-- Existing development/test wallets predate OIDC. We do not invent ownership.
-- We only backfill a wallet when its external wagering history points to exactly
-- one distinct non-empty provider. Ambiguous or history-less legacy wallets stay
-- NULL and production HTTP access to them is denied until explicitly migrated.

ALTER TABLE wallets
    ADD COLUMN provider_id TEXT;

WITH resolved_provider AS (
    SELECT
        wallet_id,
        MIN(provider_id) AS provider_id
    FROM wager_transactions
    WHERE provider_id IS NOT NULL
      AND btrim(provider_id) <> ''
    GROUP BY wallet_id
    HAVING COUNT(DISTINCT provider_id) = 1
)
UPDATE wallets AS w
SET provider_id = rp.provider_id
FROM resolved_provider AS rp
WHERE rp.wallet_id = w.id;

ALTER TABLE wallets
    ADD CONSTRAINT wallets_provider_id_not_empty
    CHECK (
        provider_id IS NULL
        OR btrim(provider_id) <> ''
    );

CREATE INDEX wallets_provider_id_idx
    ON wallets (provider_id, id)
    WHERE provider_id IS NOT NULL;

-- This index documents and enforces the intended tenant identity for newly
-- provider-owned wallets. The original player/currency uniqueness constraint is
-- intentionally left intact in this challenge migration to preserve compatibility
-- with the existing schema and historical test data.
CREATE UNIQUE INDEX wallets_provider_player_currency_unique
    ON wallets (provider_id, player_id, currency)
    WHERE provider_id IS NOT NULL;
