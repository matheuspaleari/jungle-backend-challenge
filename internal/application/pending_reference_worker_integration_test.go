package application

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

func TestPendingReferenceWorkerResolvesAfterOriginalArrivesIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	pendingFixture := fixture
	pendingFixture.transactionID = newApplicationUUID()
	pendingFixture.externalID = "early-refund-" + fixture.suffix
	pendingFixture.idempotencyKey = "early-refund-idem-" + fixture.suffix
	refExternal := "late-bet-" + fixture.suffix

	refund := newReferencedApplicationWager(
		t, pendingFixture, domain.WagerKindRefund, "25.00",
		"early-refund", refExternal,
	)
	if _, err := processor.Process(ctx, refund, "corr-pending-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	originalFixture := fixture
	originalFixture.transactionID = newApplicationUUID()
	originalFixture.externalID = refExternal
	originalFixture.idempotencyKey = "late-bet-idem-" + fixture.suffix
	bet := newApplicationWager(t, originalFixture, domain.WagerKindBet, "25.00", "late-bet")
	if _, err := processor.Process(ctx, bet, "corr-bet-"+fixture.suffix); err != nil {
		t.Fatalf("process late BET: %v", err)
	}

	forcePendingDue(t, ctx, pool, refund.ID(), time.Now().UTC().Add(-time.Second), time.Now().UTC().Add(time.Hour))

	worker := newIntegrationPendingWorker(t, pool)
	count, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("RunOnce() count = %d, want 1", count)
	}

	assertApplicationWager(t, ctx, pool, refund.ID(), "PROCESSED", 10000)
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 1)
}

func TestPendingReferenceWorkerReschedulesMissingReferenceIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	refund := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"retry-refund", "still-missing-"+fixture.suffix,
	)
	if _, err := processor.Process(ctx, refund, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	now := time.Now().UTC()
	forcePendingDue(t, ctx, pool, refund.ID(), now.Add(-time.Second), now.Add(time.Hour))

	worker := newIntegrationPendingWorker(t, pool)
	count, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var attempts int
	var next time.Time
	err = pool.QueryRow(ctx, `
		SELECT reference_attempt_count, reference_next_attempt_at
		FROM wager_transactions WHERE id = $1
	`, refund.ID()).Scan(&attempts, &next)
	if err != nil {
		t.Fatalf("query retry state: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if !next.After(now) {
		t.Fatalf("next attempt %s must be after %s", next, now)
	}
}

func TestPendingReferenceWorkerExpiresDurablyIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	refund := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"expired-refund", "never-arrives-"+fixture.suffix,
	)
	if _, err := processor.Process(ctx, refund, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	now := time.Now().UTC()
	forcePendingDue(t, ctx, pool, refund.ID(), now.Add(-2*time.Second), now.Add(-time.Second))

	worker := newIntegrationPendingWorker(t, pool)
	count, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var state, failure string
	var nextNil, expiresNil bool
	err = pool.QueryRow(ctx, `
		SELECT state, failure_code,
		       reference_next_attempt_at IS NULL,
		       reference_expires_at IS NULL
		FROM wager_transactions WHERE id = $1
	`, refund.ID()).Scan(&state, &failure, &nextNil, &expiresNil)
	if err != nil {
		t.Fatalf("query expired state: %v", err)
	}
	if state != "REJECTED" || failure != FailureCodeReferenceExpired {
		t.Fatalf("state=%s failure=%s", state, failure)
	}
	if !nextNil || !expiresNil {
		t.Fatal("terminal reference schedule was not cleared")
	}
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 0)
}

func TestPendingReferenceWorkersDoNotDoubleProcessIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	pendingFixture := fixture
	pendingFixture.transactionID = newApplicationUUID()
	pendingFixture.externalID = "parallel-refund-" + fixture.suffix
	pendingFixture.idempotencyKey = "parallel-refund-idem-" + fixture.suffix
	refExternal := "parallel-bet-" + fixture.suffix
	refund := newReferencedApplicationWager(
		t, pendingFixture, domain.WagerKindRefund, "25.00",
		"parallel-refund", refExternal,
	)
	if _, err := processor.Process(ctx, refund, "corr-pending-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	originalFixture := fixture
	originalFixture.transactionID = newApplicationUUID()
	originalFixture.externalID = refExternal
	originalFixture.idempotencyKey = "parallel-bet-idem-" + fixture.suffix
	bet := newApplicationWager(t, originalFixture, domain.WagerKindBet, "25.00", "parallel-bet")
	if _, err := processor.Process(ctx, bet, "corr-bet-"+fixture.suffix); err != nil {
		t.Fatalf("process BET: %v", err)
	}
	forcePendingDue(t, ctx, pool, refund.ID(), time.Now().UTC().Add(-time.Second), time.Now().UTC().Add(time.Hour))

	worker1 := newIntegrationPendingWorker(t, pool)
	worker2 := newIntegrationPendingWorker(t, pool)
	errs := make(chan error, 2)
	go func() { _, err := worker1.RunOnce(ctx); errs <- err }()
	go func() { _, err := worker2.RunOnce(ctx); errs <- err }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("parallel worker error: %v", err)
		}
	}
	assertApplicationWager(t, ctx, pool, refund.ID(), "PROCESSED", 10000)
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 1)
}

func newIntegrationPendingWorker(
	t *testing.T,
	pool interface{ Close() },
) *PendingReferenceWorker {
	t.Helper()
	pgPool, ok := pool.(*pgxpool.Pool)
	if !ok {
		t.Fatal("unexpected pool type")
	}
	txManager, err := postgresinfra.NewTransactionManager(pgPool)
	if err != nil {
		t.Fatal(err)
	}
	wallets, err := postgresinfra.NewWalletRepository(pgPool)
	if err != nil {
		t.Fatal(err)
	}
	wagers, err := postgresinfra.NewWagerRepository(pgPool)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewPendingReferenceWorker(
		txManager, wallets, wagers,
		postgresinfra.NewLedgerRepository(),
		postgresinfra.NewOutboxRepository(),
		newApplicationUUID, func() time.Time { return time.Now().UTC() }, 10,
	)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func isolatePendingReferenceWorkerTest(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	future := time.Now().UTC().Add(48 * time.Hour)

	_, err := pool.Exec(
		ctx,
		`
			UPDATE wager_transactions
			SET
				reference_next_attempt_at = $1,
				reference_expires_at = CASE
					WHEN reference_expires_at IS NULL
					  OR reference_expires_at < $1
					THEN $1
					ELSE reference_expires_at
				END
			WHERE state = 'PENDING_REFERENCE'
		`,
		future,
	)
	if err != nil {
		t.Fatalf("isolate pending-reference test: %v", err)
	}
}

func forcePendingDue(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
	next time.Time,
	expires time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE wager_transactions
		SET reference_next_attempt_at = $1,
		    reference_expires_at = $2
		WHERE id = $3
	`, next, expires, id)
	if err != nil {
		t.Fatalf("force pending due: %v", err)
	}
}
