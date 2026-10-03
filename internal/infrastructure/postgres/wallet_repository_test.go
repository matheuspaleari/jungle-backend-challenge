package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

const walletRepositoryTestID = "80000000-0000-0000-0000-000000000001"

func TestWalletRepositoryGetByIDIntegration(t *testing.T) {
	pool := newWalletRepositoryIntegrationPool(t)
	ctx := context.Background()

	prepareWalletRepositoryTestData(t, ctx, pool)

	repository, err := NewWalletRepository(pool)
	if err != nil {
		t.Fatalf("NewWalletRepository() error = %v", err)
	}

	wallet, err := repository.GetByID(
		ctx,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if wallet.ID() != walletRepositoryTestID {
		t.Fatalf(
			"wallet ID = %q, want %q",
			wallet.ID(),
			walletRepositoryTestID,
		)
	}

	if wallet.PlayerID() != "wallet-repository-player" {
		t.Fatalf(
			"player ID = %q, want %q",
			wallet.PlayerID(),
			"wallet-repository-player",
		)
	}

	if wallet.Currency() != "BRL" {
		t.Fatalf(
			"currency = %q, want BRL",
			wallet.Currency(),
		)
	}

	if wallet.Balance().MinorUnits() != 10000 {
		t.Fatalf(
			"balance = %d, want 10000",
			wallet.Balance().MinorUnits(),
		)
	}

	if wallet.Version() != 1 {
		t.Fatalf(
			"version = %d, want 1",
			wallet.Version(),
		)
	}
}

func TestWalletRepositoryGetByIDForUpdateBlocksConcurrentTransaction(
	t *testing.T,
) {
	pool := newWalletRepositoryIntegrationPool(t)
	ctx := context.Background()

	prepareWalletRepositoryTestData(t, ctx, pool)

	repository, err := NewWalletRepository(pool)
	if err != nil {
		t.Fatalf("NewWalletRepository() error = %v", err)
	}

	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}

	tx1Finished := false

	defer func() {
		if !tx1Finished {
			_ = tx1.Rollback(context.Background())
		}
	}()

	_, err = repository.GetByIDForUpdate(
		ctx,
		tx1,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"tx1 GetByIDForUpdate() error = %v",
			err,
		)
	}

	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}

	tx2Finished := false

	defer func() {
		if !tx2Finished {
			_ = tx2.Rollback(context.Background())
		}
	}()

	type lockResult struct {
		err error
	}

	resultChannel := make(chan lockResult, 1)

	go func() {
		_, err := repository.GetByIDForUpdate(
			context.Background(),
			tx2,
			walletRepositoryTestID,
		)

		resultChannel <- lockResult{
			err: err,
		}
	}()

	select {
	case result := <-resultChannel:
		t.Fatalf(
			"tx2 acquired wallet lock before tx1 released it; error = %v",
			result.err,
		)

	case <-time.After(250 * time.Millisecond):
		// Esperado:
		// tx2 continua bloqueada esperando tx1 liberar a row lock.
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	tx1Finished = true

	select {
	case result := <-resultChannel:
		if result.err != nil {
			t.Fatalf(
				"tx2 GetByIDForUpdate() after tx1 commit error = %v",
				result.err,
			)
		}

	case <-time.After(2 * time.Second):
		t.Fatal(
			"tx2 did not acquire wallet lock after tx1 committed",
		)
	}

	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit tx2: %v", err)
	}

	tx2Finished = true
}

func TestWalletRepositoryUpdateIntegration(t *testing.T) {
	pool := newWalletRepositoryIntegrationPool(t)
	ctx := context.Background()

	prepareWalletRepositoryTestData(t, ctx, pool)

	repository, err := NewWalletRepository(pool)
	if err != nil {
		t.Fatalf("NewWalletRepository() error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	finished := false

	defer func() {
		if !finished {
			_ = tx.Rollback(context.Background())
		}
	}()

	wallet, err := repository.GetByIDForUpdate(
		ctx,
		tx,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"GetByIDForUpdate() error = %v",
			err,
		)
	}

	debit, err := domain.NewMoney(
		"20.00",
		"BRL",
	)
	if err != nil {
		t.Fatalf(
			"NewMoney() error = %v",
			err,
		)
	}

	if err := wallet.Debit(
		debit,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf(
			"wallet.Debit() error = %v",
			err,
		)
	}

	if err := repository.Update(
		ctx,
		tx,
		wallet,
	); err != nil {
		t.Fatalf(
			"repository.Update() error = %v",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf(
			"commit transaction: %v",
			err,
		)
	}

	finished = true

	persistedWallet, err := repository.GetByID(
		ctx,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"GetByID() after update error = %v",
			err,
		)
	}

	if persistedWallet.Balance().MinorUnits() != 8000 {
		t.Fatalf(
			"balance = %d, want 8000",
			persistedWallet.Balance().MinorUnits(),
		)
	}

	if persistedWallet.Version() != 2 {
		t.Fatalf(
			"version = %d, want 2",
			persistedWallet.Version(),
		)
	}
}

func TestWalletRepositoryUpdateRejectsStaleVersionIntegration(
	t *testing.T,
) {
	pool := newWalletRepositoryIntegrationPool(t)
	ctx := context.Background()

	prepareWalletRepositoryTestData(t, ctx, pool)

	repository, err := NewWalletRepository(pool)
	if err != nil {
		t.Fatalf("NewWalletRepository() error = %v", err)
	}

	staleWallet, err := repository.GetByID(
		ctx,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"load stale wallet: %v",
			err,
		)
	}

	_, err = pool.Exec(
		ctx,
		`
			UPDATE wallets
			SET
				balance_minor = 9000,
				version = 2,
				updated_at = NOW()
			WHERE id = $1
		`,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"simulate concurrent wallet update: %v",
			err,
		)
	}

	debit, err := domain.NewMoney(
		"20.00",
		"BRL",
	)
	if err != nil {
		t.Fatalf(
			"NewMoney() error = %v",
			err,
		)
	}

	if err := staleWallet.Debit(
		debit,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf(
			"stale wallet Debit() error = %v",
			err,
		)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf(
			"begin transaction: %v",
			err,
		)
	}

	finished := false

	defer func() {
		if !finished {
			_ = tx.Rollback(context.Background())
		}
	}()

	err = repository.Update(
		ctx,
		tx,
		staleWallet,
	)

	if !errors.Is(err, ErrWalletVersionConflict) {
		t.Fatalf(
			"Update() error = %v, want ErrWalletVersionConflict",
			err,
		)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf(
			"rollback transaction: %v",
			err,
		)
	}

	finished = true

	persistedWallet, err := repository.GetByID(
		ctx,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"GetByID() after conflict error = %v",
			err,
		)
	}

	if persistedWallet.Balance().MinorUnits() != 9000 {
		t.Fatalf(
			"balance after conflict = %d, want 9000",
			persistedWallet.Balance().MinorUnits(),
		)
	}

	if persistedWallet.Version() != 2 {
		t.Fatalf(
			"version after conflict = %d, want 2",
			persistedWallet.Version(),
		)
	}
}

func TestNewWalletRepositoryRequiresPool(t *testing.T) {
	_, err := NewWalletRepository(nil)

	if err == nil {
		t.Fatal(
			"NewWalletRepository(nil) expected error",
		)
	}
}

func prepareWalletRepositoryTestData(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM wallets
			WHERE id = $1
		`,
		walletRepositoryTestID,
	)
	if err != nil {
		t.Fatalf(
			"clean wallet repository test data: %v",
			err,
		)
	}

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO wallets (
				id,
				player_id,
				currency,
				balance_minor,
				version,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				$5,
				NOW(),
				NOW()
			)
		`,
		walletRepositoryTestID,
		"wallet-repository-player",
		"BRL",
		int64(10000),
		int64(1),
	)
	if err != nil {
		t.Fatalf(
			"insert wallet repository test data: %v",
			err,
		)
	}
}

func newWalletRepositoryIntegrationPool(
	t *testing.T,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")

	if databaseURL == "" {
		t.Skip(
			"TEST_DATABASE_URL not configured",
		)
	}

	pool, err := NewPool(
		context.Background(),
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"NewPool() error = %v",
			err,
		)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}
