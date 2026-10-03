package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

var (
	ErrOutboxRepositoryTxRequired = errors.New("outbox repository transaction is required")
	ErrOutboxPoolRequired         = errors.New("outbox repository pool is required")
	ErrOutboxLeaseLost            = errors.New("outbox lease lost")
)

type OutboxRepository struct {
	pool *pgxpool.Pool
}

type OutboxRecord struct {
	ID            string
	EventType     string
	AggregateID   string
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
	EventVersion  uint64
	Payload       json.RawMessage
	RetryCount    int
	NextAttemptAt time.Time
}

// The variadic form preserves the old constructor usage in tests while allowing
// the publisher to use the repository outside an already-open transaction.
func NewOutboxRepository(pools ...*pgxpool.Pool) *OutboxRepository {
	var pool *pgxpool.Pool
	if len(pools) > 0 {
		pool = pools[0]
	}
	return &OutboxRepository{pool: pool}
}

func (r *OutboxRepository) Insert(ctx context.Context, tx pgx.Tx, event *domain.Event) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if tx == nil {
		return ErrOutboxRepositoryTxRequired
	}
	if event == nil {
		return errors.New("event is required")
	}

	payload, err := json.Marshal(event.Data())
	if err != nil {
		return fmt.Errorf("marshal outbox event payload: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			id, event_type, aggregate_id, correlation_id, causation_id,
			occurred_at, event_version, payload, retry_count,
			next_attempt_at, published_at, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,0,$6,NULL,$6)
	`,
		event.EventID(), string(event.EventType()), event.AggregateID(),
		event.CorrelationID(), nullableString(event.CausationID()),
		event.OccurredAt(), event.Version(), payload,
	)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// ClaimPending atomically leases pending rows. FOR UPDATE SKIP LOCKED lets
// multiple publisher instances compete without waiting for each other.
func (r *OutboxRepository) ClaimPending(
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
	limit int,
) ([]OutboxRecord, error) {
	if r.pool == nil {
		return nil, ErrOutboxPoolRequired
	}
	if workerID == "" {
		return nil, errors.New("worker id is required")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("lease duration must be positive")
	}
	if limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}

	leaseUntil := now.UTC().Add(leaseDuration)
	rows, err := r.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL
			  AND next_attempt_at <= $1
			  AND (locked_until IS NULL OR locked_until <= $1)
			ORDER BY next_attempt_at, occurred_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE outbox_events AS o
		SET locked_by = $3,
			locked_until = $4
		FROM candidates AS c
		WHERE o.id = c.id
		RETURNING
			o.id::text, o.event_type, o.aggregate_id, o.correlation_id,
			COALESCE(o.causation_id, ''), o.occurred_at, o.event_version,
			o.payload, o.retry_count, o.next_attempt_at
	`, now.UTC(), limit, workerID, leaseUntil)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	records := make([]OutboxRecord, 0, limit)
	for rows.Next() {
		var record OutboxRecord
		if err := rows.Scan(
			&record.ID, &record.EventType, &record.AggregateID,
			&record.CorrelationID, &record.CausationID, &record.OccurredAt,
			&record.EventVersion, &record.Payload, &record.RetryCount,
			&record.NextAttemptAt,
		); err != nil {
			return nil, fmt.Errorf("scan claimed outbox event: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed outbox events: %w", err)
	}
	return records, nil
}

func (r *OutboxRepository) MarkPublished(
	ctx context.Context,
	eventID string,
	workerID string,
	publishedAt time.Time,
) error {
	if r.pool == nil {
		return ErrOutboxPoolRequired
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = $3,
			locked_by = NULL,
			locked_until = NULL,
			last_error = NULL
		WHERE id = $1::uuid
		  AND published_at IS NULL
		  AND locked_by = $2
	`, eventID, workerID, publishedAt.UTC())
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrOutboxLeaseLost
	}
	return nil
}

func (r *OutboxRepository) ScheduleRetry(
	ctx context.Context,
	eventID string,
	workerID string,
	nextAttemptAt time.Time,
	lastError string,
) error {
	if r.pool == nil {
		return ErrOutboxPoolRequired
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET retry_count = retry_count + 1,
			next_attempt_at = $3,
			locked_by = NULL,
			locked_until = NULL,
			last_error = NULLIF(left($4, 1000), '')
		WHERE id = $1::uuid
		  AND published_at IS NULL
		  AND locked_by = $2
	`, eventID, workerID, nextAttemptAt.UTC(), lastError)
	if err != nil {
		return fmt.Errorf("schedule outbox retry: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrOutboxLeaseLost
	}
	return nil
}
