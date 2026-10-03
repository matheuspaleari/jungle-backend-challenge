package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInboxMessageConflict = errors.New("inbox message id reused with different payload")
	ErrInboxPoolRequired    = errors.New("inbox repository postgres pool is required")
	ErrInboxTxRequired      = errors.New("inbox repository transaction is required")
)

type InboxRepository struct{ pool *pgxpool.Pool }

type InboxReceipt struct {
	Completed bool
	Duplicate bool
}

func NewInboxRepository(pool *pgxpool.Pool) (*InboxRepository, error) {
	if pool == nil {
		return nil, ErrInboxPoolRequired
	}
	return &InboxRepository{pool: pool}, nil
}

// Receive is kept for repository-level callers/tests. SQS processing must use
// ReceiveTx so inbox state shares the financial transaction.
func (r *InboxRepository) Receive(ctx context.Context, id, consumerName, messageID, payloadHash string, now time.Time) (InboxReceipt, error) {
	if ctx == nil {
		return InboxReceipt{}, errors.New("context is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InboxReceipt{}, fmt.Errorf("begin inbox receive: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	receipt, err := r.ReceiveTx(ctx, tx, id, consumerName, messageID, payloadHash, now)
	if err != nil {
		return InboxReceipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InboxReceipt{}, fmt.Errorf("commit inbox receive: %w", err)
	}
	return receipt, nil
}

func (r *InboxRepository) ReceiveTx(ctx context.Context, tx pgx.Tx, id, consumerName, messageID, payloadHash string, now time.Time) (InboxReceipt, error) {
	if ctx == nil {
		return InboxReceipt{}, errors.New("context is required")
	}
	if tx == nil {
		return InboxReceipt{}, ErrInboxTxRequired
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(consumerName) == "" ||
		strings.TrimSpace(messageID) == "" || strings.TrimSpace(payloadHash) == "" {
		return InboxReceipt{}, errors.New("inbox identity fields are required")
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO inbox_messages (id, consumer_name, message_id, payload_hash, received_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (consumer_name, message_id) DO NOTHING
	`, id, consumerName, messageID, payloadHash, now.UTC())
	if err != nil {
		return InboxReceipt{}, fmt.Errorf("insert inbox message: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return InboxReceipt{}, nil
	}

	var storedHash string
	var completedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT payload_hash, completed_at
		FROM inbox_messages
		WHERE consumer_name = $1 AND message_id = $2
	`, consumerName, messageID).Scan(&storedHash, &completedAt)
	if err != nil {
		return InboxReceipt{}, fmt.Errorf("read inbox message: %w", err)
	}
	if storedHash != payloadHash {
		return InboxReceipt{}, ErrInboxMessageConflict
	}
	return InboxReceipt{Completed: completedAt != nil, Duplicate: true}, nil
}

// Complete is kept for repository-level callers/tests. SQS processing must use
// CompleteTx in the same transaction used by ReceiveTx and the domain write.
func (r *InboxRepository) Complete(ctx context.Context, consumerName, messageID string, now time.Time) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin inbox completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := r.CompleteTx(ctx, tx, consumerName, messageID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inbox completion: %w", err)
	}
	return nil
}

func (r *InboxRepository) CompleteTx(ctx context.Context, tx pgx.Tx, consumerName, messageID string, now time.Time) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if tx == nil {
		return ErrInboxTxRequired
	}
	tag, err := tx.Exec(ctx, `
		UPDATE inbox_messages
		SET completed_at = COALESCE(completed_at, $3)
		WHERE consumer_name = $1 AND message_id = $2
	`, consumerName, messageID, now.UTC())
	if err != nil {
		return fmt.Errorf("complete inbox message: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("inbox message not found")
	}
	return nil
}
