package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTransactionManagerCommitIntegration(t *testing.T) {
	pool := newTransactionIntegrationPool(t)

	manager, err := NewTransactionManager(pool)
	if err != nil {
		t.Fatalf("NewTransactionManager() error = %v", err)
	}

	ctx := context.Background()

	_, err = pool.Exec(
		ctx,
		`
			CREATE TABLE IF NOT EXISTS integration_transaction_test (
				test_id TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)
		`,
	)
	if err != nil {
		t.Fatalf("create integration table: %v", err)
	}

	testID := "commit-test"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM integration_transaction_test
			WHERE test_id = $1
		`,
		testID,
	)
	if err != nil {
		t.Fatalf("clean integration test: %v", err)
	}

	err = manager.WithinTransaction(
		ctx,
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(
				ctx,
				`
					INSERT INTO integration_transaction_test (
						test_id,
						value
					)
					VALUES (
						$1,
						$2
					)
				`,
				testID,
				"committed",
			)

			return err
		},
	)
	if err != nil {
		t.Fatalf("WithinTransaction() error = %v", err)
	}

	var value string

	err = pool.QueryRow(
		ctx,
		`
			SELECT value
			FROM integration_transaction_test
			WHERE test_id = $1
		`,
		testID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query committed value: %v", err)
	}

	if value != "committed" {
		t.Fatalf(
			"value = %q, want %q",
			value,
			"committed",
		)
	}
}

func TestTransactionManagerRollbackIntegration(t *testing.T) {
	pool := newTransactionIntegrationPool(t)

	manager, err := NewTransactionManager(pool)
	if err != nil {
		t.Fatalf("NewTransactionManager() error = %v", err)
	}

	ctx := context.Background()

	_, err = pool.Exec(
		ctx,
		`
			CREATE TABLE IF NOT EXISTS integration_transaction_test (
				test_id TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)
		`,
	)
	if err != nil {
		t.Fatalf("create integration table: %v", err)
	}

	testID := "rollback-test"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM integration_transaction_test
			WHERE test_id = $1
		`,
		testID,
	)
	if err != nil {
		t.Fatalf("clean integration test: %v", err)
	}

	expectedErr := errors.New("force rollback")

	err = manager.WithinTransaction(
		ctx,
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(
				ctx,
				`
					INSERT INTO integration_transaction_test (
						test_id,
						value
					)
					VALUES (
						$1,
						$2
					)
				`,
				testID,
				"must-rollback",
			)
			if err != nil {
				return err
			}

			return expectedErr
		},
	)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"WithinTransaction() error = %v, want %v",
			err,
			expectedErr,
		)
	}

	var count int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM integration_transaction_test
			WHERE test_id = $1
		`,
		testID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query rolled back value: %v", err)
	}

	if count != 0 {
		t.Fatalf(
			"rolled back rows = %d, want 0",
			count,
		)
	}
}

func TestNewTransactionManagerRequiresPool(t *testing.T) {
	_, err := NewTransactionManager(nil)

	if err == nil {
		t.Fatal(
			"NewTransactionManager(nil) expected error",
		)
	}
}

func newTransactionIntegrationPool(
	t *testing.T,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")

	if databaseURL == "" {
		t.Skip(
			"TEST_DATABASE_URL not configured",
		)
	}

	ctx := context.Background()

	pool, err := NewPool(
		ctx,
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
