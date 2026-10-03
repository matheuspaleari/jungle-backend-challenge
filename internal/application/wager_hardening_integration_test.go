package application

import (
	"context"
	"testing"
	"time"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestPendingReferenceWorkerRejectsAfterMaxAttemptsIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	refund := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"max-attempt-refund", "missing-max-attempt-"+fixture.suffix,
	)
	if _, err := processor.Process(ctx, refund, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		UPDATE wager_transactions
		SET reference_attempt_count = $1,
		    reference_next_attempt_at = $2,
		    reference_expires_at = $3
		WHERE id = $4
	`, PendingReferenceMaxAttempts-1, now.Add(-time.Second), now.Add(time.Hour), refund.ID())
	if err != nil {
		t.Fatalf("prepare max-attempt pending reference: %v", err)
	}

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
		FROM wager_transactions
		WHERE id = $1
	`, refund.ID()).Scan(&state, &failure, &nextNil, &expiresNil)
	if err != nil {
		t.Fatalf("query max-attempt result: %v", err)
	}
	if state != "REJECTED" || failure != FailureCodeReferenceRetryExhausted {
		t.Fatalf("state=%s failure=%s", state, failure)
	}
	if !nextNil || !expiresNil {
		t.Fatal("retry schedule was not cleared after max attempts")
	}
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 0)
}

func TestPendingReferenceWorkerReschedulesWhenReferenceExistsButIsPendingIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	refExternal := "pending-original-" + fixture.suffix
	refund := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"pending-original-refund", refExternal,
	)
	if _, err := processor.Process(ctx, refund, "corr-refund-"+fixture.suffix); err != nil {
		t.Fatalf("create pending REFUND: %v", err)
	}

	pendingReferenceID := newApplicationUUID()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO wager_transactions (
			id, external_transaction_id, provider_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind,
			amount_minor, currency, reference_external_transaction_id,
			resolved_reference_transaction_id, state, failure_code,
			result_balance_minor, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, 'application-game', 'BET',
			2500, 'BRL', NULL,
			NULL, 'PENDING', NULL,
			NULL, $9, $9
		)
	`, pendingReferenceID, refExternal, fixture.providerID,
		"pending-original-idem-"+fixture.suffix,
		"pending-original-hash-"+fixture.suffix,
		fixture.walletID, fixture.playerID, fixture.roundID, now)
	if err != nil {
		t.Fatalf("insert pending reference transaction: %v", err)
	}

	forcePendingDue(t, ctx, pool, refund.ID(), now.Add(-time.Second), now.Add(time.Hour))

	worker := newIntegrationPendingWorker(t, pool)
	count, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var state, failure string
	var attempts int
	err = pool.QueryRow(ctx, `
		SELECT state, COALESCE(failure_code, ''), reference_attempt_count
		FROM wager_transactions
		WHERE id = $1
	`, refund.ID()).Scan(&state, &failure, &attempts)
	if err != nil {
		t.Fatalf("query pending reference result: %v", err)
	}
	if state != "PENDING_REFERENCE" {
		t.Fatalf("state = %s, want PENDING_REFERENCE", state)
	}
	if failure != "" {
		t.Fatalf("failure = %s, want empty", failure)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRollbackInsufficientFundsUsesDistinctFailureCodeIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 0)
	processor := newIntegrationWagerProcessor(t, pool)

	winFixture := fixture
	winFixture.transactionID = newApplicationUUID()
	winFixture.externalID = "win-for-rollback-" + fixture.suffix
	winFixture.idempotencyKey = "win-for-rollback-idem-" + fixture.suffix
	win := newApplicationWager(t, winFixture, domain.WagerKindWin, "25.00", "win-for-rollback")
	if _, err := processor.Process(ctx, win, "corr-win-"+fixture.suffix); err != nil {
		t.Fatalf("process WIN: %v", err)
	}

	betFixture := fixture
	betFixture.transactionID = newApplicationUUID()
	betFixture.externalID = "drain-bet-" + fixture.suffix
	betFixture.idempotencyKey = "drain-bet-idem-" + fixture.suffix
	bet := newApplicationWager(t, betFixture, domain.WagerKindBet, "25.00", "drain-bet")
	if _, err := processor.Process(ctx, bet, "corr-bet-"+fixture.suffix); err != nil {
		t.Fatalf("process draining BET: %v", err)
	}

	rollbackFixture := fixture
	rollbackFixture.transactionID = newApplicationUUID()
	rollbackFixture.externalID = "rollback-win-" + fixture.suffix
	rollbackFixture.idempotencyKey = "rollback-win-idem-" + fixture.suffix
	rollback := newReferencedApplicationWager(
		t, rollbackFixture, domain.WagerKindRollback, "25.00",
		"rollback-win", winFixture.externalID,
	)
	result, err := processor.Process(ctx, rollback, "corr-rollback-"+fixture.suffix)
	if err != nil {
		t.Fatalf("process ROLLBACK: %v", err)
	}
	if result.Transaction.Status() != domain.WagerStatusRejected {
		t.Fatalf("status = %s, want REJECTED", result.Transaction.Status())
	}
	if result.Transaction.FailureCode() != FailureCodeReversalInsufficientFunds {
		t.Fatalf("failure = %s, want %s", result.Transaction.FailureCode(), FailureCodeReversalInsufficientFunds)
	}
	assertApplicationWallet(t, ctx, pool, fixture.walletID, 0, 3)
	assertApplicationLedgerCount(t, ctx, pool, rollback.ID(), 0)
}

func TestConcurrentRefundAndRollbackAllowOnlyOneSuccessfulReversalIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	betFixture := fixture
	betFixture.transactionID = newApplicationUUID()
	betFixture.externalID = "cross-reversal-bet-" + fixture.suffix
	betFixture.idempotencyKey = "cross-reversal-bet-idem-" + fixture.suffix
	bet := newApplicationWager(t, betFixture, domain.WagerKindBet, "25.00", "cross-reversal-bet")
	if _, err := processor.Process(ctx, bet, "corr-bet-"+fixture.suffix); err != nil {
		t.Fatalf("process BET: %v", err)
	}

	refundFixture := fixture
	refundFixture.transactionID = newApplicationUUID()
	refundFixture.externalID = "cross-refund-" + fixture.suffix
	refundFixture.idempotencyKey = "cross-refund-idem-" + fixture.suffix
	refund := newReferencedApplicationWager(
		t, refundFixture, domain.WagerKindRefund, "25.00",
		"cross-refund", betFixture.externalID,
	)

	rollbackFixture := fixture
	rollbackFixture.transactionID = newApplicationUUID()
	rollbackFixture.externalID = "cross-rollback-" + fixture.suffix
	rollbackFixture.idempotencyKey = "cross-rollback-idem-" + fixture.suffix
	rollback := newReferencedApplicationWager(
		t, rollbackFixture, domain.WagerKindRollback, "25.00",
		"cross-rollback", betFixture.externalID,
	)

	type processResult struct {
		result *ProcessWagerResult
		err    error
	}
	results := make(chan processResult, 2)
	go func() {
		result, err := processor.Process(ctx, refund, "corr-refund-"+fixture.suffix)
		results <- processResult{result: result, err: err}
	}()
	go func() {
		result, err := processor.Process(ctx, rollback, "corr-rollback-"+fixture.suffix)
		results <- processResult{result: result, err: err}
	}()

	processed := 0
	rejected := 0
	for i := 0; i < 2; i++ {
		item := <-results
		if item.err != nil {
			t.Fatalf("concurrent reversal error: %v", item.err)
		}
		switch item.result.Transaction.Status() {
		case domain.WagerStatusProcessed:
			processed++
		case domain.WagerStatusRejected:
			if item.result.Transaction.FailureCode() != FailureCodeAlreadyReversed {
				t.Fatalf("rejected failure = %s, want %s", item.result.Transaction.FailureCode(), FailureCodeAlreadyReversed)
			}
			rejected++
		default:
			t.Fatalf("unexpected reversal status: %s", item.result.Transaction.Status())
		}
	}

	if processed != 1 || rejected != 1 {
		t.Fatalf("processed=%d rejected=%d, want 1/1", processed, rejected)
	}
	assertApplicationWallet(t, ctx, pool, fixture.walletID, 10000, 3)

	var ledgerCount int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transaction_id IN ($1, $2)
	`, refund.ID(), rollback.ID()).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count reversal ledger entries: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("reversal ledger count = %d, want 1", ledgerCount)
	}
}
