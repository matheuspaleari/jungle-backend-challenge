package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestWalletRepositoryAllowsSamePlayerAndCurrencyAcrossProvidersIntegration(
	t *testing.T,
) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	const (
		walletA  = "8f140000-0000-4000-8000-000000000001"
		walletB  = "8f140000-0000-4000-8000-000000000002"
		playerID = "provider-isolation-shared-player"
	)

	cleanupProviderScopedWalletTest(t, ctx, pool, walletA, walletB)

	money, err := domain.NewMoney("0.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	now := time.Now().UTC()
	first, err := domain.NewWalletForProvider(
		walletA, "provider-a", playerID, money, now,
	)
	if err != nil {
		t.Fatalf("NewWalletForProvider(provider-a) error = %v", err)
	}
	second, err := domain.NewWalletForProvider(
		walletB, "provider-b", playerID, money, now,
	)
	if err != nil {
		t.Fatalf("NewWalletForProvider(provider-b) error = %v", err)
	}

	repository, err := NewWalletRepository(pool)
	if err != nil {
		t.Fatalf("NewWalletRepository() error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if err := repository.Insert(ctx, tx, first); err != nil {
		t.Fatalf("insert provider-a wallet: %v", err)
	}
	if err := repository.Insert(ctx, tx, second); err != nil {
		t.Fatalf(
			"insert provider-b wallet with same player/currency: %v; "+
				"migration 000011 must remove the legacy global uniqueness",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	committed = true

	var count int
	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM wallets
			WHERE id = ANY($1::uuid[])
			  AND player_id = $2
			  AND currency = 'BRL'
			  AND provider_id IN ('provider-a', 'provider-b')
		`,
		[]string{walletA, walletB},
		playerID,
	).Scan(&count); err != nil {
		t.Fatalf("count provider-scoped wallets: %v", err)
	}
	if count != 2 {
		t.Fatalf("provider-scoped wallet count = %d, want 2", count)
	}
}

func cleanupProviderScopedWalletTest(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	walletIDs ...string,
) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(
			context.Background(),
			`DELETE FROM wallets WHERE id = ANY($1::uuid[])`,
			walletIDs,
		)
	})

	if _, err := pool.Exec(
		ctx,
		`DELETE FROM wallets WHERE id = ANY($1::uuid[])`,
		walletIDs,
	); err != nil {
		t.Fatalf("cleanup provider-scoped wallet test: %v", err)
	}
}
