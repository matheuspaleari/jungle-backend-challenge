package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInboxReceiveAndCompleteRollbackTogetherIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	repo, err := NewInboxRepository(pool)
	if err != nil {
		t.Fatalf("new inbox repository: %v", err)
	}
	manager, err := NewTransactionManager(pool)
	if err != nil {
		t.Fatalf("new transaction manager: %v", err)
	}

	suffix := time.Now().UTC().UnixNano()
	consumer := "atomicity-test"
	messageID := fmt.Sprintf("message-%d", suffix)
	rowID := fmt.Sprintf("00000000-0000-4000-8000-%012d", suffix%1000000000000)
	now := time.Now().UTC()
	injected := errors.New("force rollback after inbox completion")

	err = manager.WithinTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if _, err := repo.ReceiveTx(txCtx, tx, rowID, consumer, messageID, "hash-a", now); err != nil {
			return err
		}
		if err := repo.CompleteTx(txCtx, tx, consumer, messageID, now); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error=%v want injected rollback", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2`, consumer, messageID).Scan(&count); err != nil {
		t.Fatalf("count rolled back inbox row: %v", err)
	}
	if count != 0 {
		t.Fatalf("inbox rows after rollback=%d want=0", count)
	}

	err = manager.WithinTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if _, err := repo.ReceiveTx(txCtx, tx, rowID, consumer, messageID, "hash-a", now); err != nil {
			return err
		}
		return repo.CompleteTx(txCtx, tx, consumer, messageID, now)
	})
	if err != nil {
		t.Fatalf("commit inbox transaction: %v", err)
	}

	var completed bool
	if err := pool.QueryRow(ctx, `SELECT completed_at IS NOT NULL FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2`, consumer, messageID).Scan(&completed); err != nil {
		t.Fatalf("read committed inbox row: %v", err)
	}
	if !completed {
		t.Fatal("committed inbox row is not completed")
	}

	_, _ = pool.Exec(ctx, `DELETE FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2`, consumer, messageID)
}
