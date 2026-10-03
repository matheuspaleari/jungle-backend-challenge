package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutboxClaimIsExclusiveAcrossPublishersAndExpiredLeaseRecovers(t *testing.T) {
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

	var hasLeaseColumn bool
	if err := pool.QueryRow(
		ctx,
		`
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name = 'outbox_events'
				  AND column_name = 'locked_until'
			)
		`,
	).Scan(&hasLeaseColumn); err != nil {
		t.Fatalf("inspect outbox schema: %v", err)
	}
	if !hasLeaseColumn {
		t.Skip("migration 000009 not applied")
	}

	/*
		Use a deterministic timestamp far in the past.

		ClaimPending selects every eligible unpublished outbox row. A developer
		database can legitimately contain pending events from previous manual
		tests, so using time.Now() here would make this integration test compete
		with unrelated application data.

		By placing the fixture in 2000 and claiming at that same logical time,
		normal project events created in 2026 are not eligible
		(next_attempt_at <= now), keeping the test isolated without deleting or
		mutating real outbox rows.
	*/
	now := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	eventID := testOutboxUUID(now.UnixNano())

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO outbox_events (
				id,
				event_type,
				aggregate_id,
				correlation_id,
				occurred_at,
				event_version,
				payload,
				retry_count,
				next_attempt_at,
				created_at
			)
			VALUES (
				$1::uuid,
				'WagerTransactionProcessed',
				$2,
				$3,
				$4,
				1,
				'{}'::jsonb,
				0,
				$4,
				$4
			)
		`,
		eventID,
		"wallet-"+eventID,
		"corr-"+eventID,
		now,
	)
	if err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	defer func() {
		if _, cleanupErr := pool.Exec(
			context.Background(),
			`DELETE FROM outbox_events WHERE id = $1::uuid`,
			eventID,
		); cleanupErr != nil {
			t.Logf("cleanup outbox fixture: %v", cleanupErr)
		}
	}()

	repoA := NewOutboxRepository(pool)
	repoB := NewOutboxRepository(pool)

	start := make(chan struct{})
	results := make(chan []OutboxRecord, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	claim := func(repo *OutboxRepository, worker string) {
		defer wg.Done()

		<-start

		records, claimErr := repo.ClaimPending(
			ctx,
			worker,
			now,
			2*time.Second,
			1,
		)
		if claimErr != nil {
			errs <- fmt.Errorf("claim %s: %w", worker, claimErr)
			return
		}

		results <- records
	}

	go claim(repoA, "publisher-a")
	go claim(repoB, "publisher-b")

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for claimErr := range errs {
		t.Fatal(claimErr)
	}

	claimedFixtureCount := 0
	for records := range results {
		for _, record := range records {
			if record.ID == eventID {
				claimedFixtureCount++
			}
		}
	}

	if claimedFixtureCount != 1 {
		t.Fatalf(
			"expected fixture event to be claimed exactly once, got %d",
			claimedFixtureCount,
		)
	}

	// Before lease expiry, another publisher must not be able to claim it.
	beforeExpiry, err := repoB.ClaimPending(
		ctx,
		"publisher-before-expiry",
		now.Add(1*time.Second),
		2*time.Second,
		1,
	)
	if err != nil {
		t.Fatalf("claim before lease expiry: %v", err)
	}

	for _, record := range beforeExpiry {
		if record.ID == eventID {
			t.Fatalf("event was reclaimed before lease expiry")
		}
	}

	// Once the lease expires, abandoned work must be claimable by another instance.
	recovered, err := repoB.ClaimPending(
		ctx,
		"publisher-recovery",
		now.Add(3*time.Second),
		2*time.Second,
		1,
	)
	if err != nil {
		t.Fatalf("recover expired lease: %v", err)
	}

	if len(recovered) != 1 || recovered[0].ID != eventID {
		t.Fatalf("expected abandoned event recovery, got %#v", recovered)
	}

	if err := repoB.MarkPublished(
		ctx,
		eventID,
		"publisher-recovery",
		now.Add(3*time.Second),
	); err != nil {
		t.Fatalf("mark recovered event published: %v", err)
	}

	var (
		publishedAt time.Time
		lockedBy    *string
		lockedUntil *time.Time
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT published_at, locked_by, locked_until
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		eventID,
	).Scan(&publishedAt, &lockedBy, &lockedUntil); err != nil {
		t.Fatalf("read published event: %v", err)
	}

	if !publishedAt.Equal(now.Add(3 * time.Second)) {
		t.Fatalf(
			"unexpected published_at: got %s want %s",
			publishedAt,
			now.Add(3*time.Second),
		)
	}

	if lockedBy != nil || lockedUntil != nil {
		t.Fatalf(
			"expected lease to be cleared after publish, locked_by=%v locked_until=%v",
			lockedBy,
			lockedUntil,
		)
	}
}

func testOutboxUUID(seed int64) string {
	value := uint64(seed)

	return fmt.Sprintf(
		"%08x-%04x-4%03x-8%03x-%012x",
		uint32(value),
		uint16(value>>32),
		uint16(value>>16)&0x0fff,
		uint16(value)&0x0fff,
		value&0xffffffffffff,
	)
}
