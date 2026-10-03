package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestWagerProcessorConcurrentIdenticalRequestsAreIdempotentIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWagerWithHash(
		t, fixture, domain.WagerKindBet, "25.00", "idem-concurrent-1", "same-payload",
	)
	secondFixture := fixture
	secondFixture.transactionID = newApplicationUUID()
	second := newApplicationWagerWithHash(
		t, secondFixture, domain.WagerKindBet, "25.00", "idem-concurrent-2", "same-payload",
	)

	start := make(chan struct{})
	type outcome struct {
		result *ProcessWagerResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	run := func(tx *domain.WagerTransaction) {
		defer wg.Done()
		<-start
		result, err := processor.Process(ctx, tx, "corr-idem-"+fixture.suffix)
		outcomes <- outcome{result: result, err: err}
	}

	go run(first)
	go run(second)
	close(start)
	wg.Wait()
	close(outcomes)

	replays := 0
	fresh := 0
	for item := range outcomes {
		if item.err != nil {
			t.Fatalf("concurrent Process() error = %v", item.err)
		}
		if item.result == nil {
			t.Fatal("concurrent Process() returned nil result")
		}
		if item.result.Replay {
			replays++
		} else {
			fresh++
		}
		if item.result.Balance.MinorUnits() != 7500 {
			t.Fatalf("balance = %d, want 7500", item.result.Balance.MinorUnits())
		}
	}

	if fresh != 1 || replays != 1 {
		t.Fatalf("fresh=%d replay=%d, want 1/1", fresh, replays)
	}
	assertApplicationWallet(t, ctx, pool, fixture.walletID, 7500, 2)

	var wagerCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM wager_transactions
		WHERE provider_id = $1 AND idempotency_key = $2
	`, fixture.providerID, fixture.idempotencyKey).Scan(&wagerCount); err != nil {
		t.Fatalf("count idempotent wagers: %v", err)
	}
	if wagerCount != 1 {
		t.Fatalf("wager count = %d, want 1", wagerCount)
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = $1
	`, fixture.walletID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger count = %d, want 1", ledgerCount)
	}
}

func TestWagerProcessorExternalTransactionConflictIsStableIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWagerWithHash(
		t, fixture, domain.WagerKindBet, "25.00", "external-first", "same-body",
	)
	if _, err := processor.Process(ctx, first, "corr-first-"+fixture.suffix); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}

	secondFixture := fixture
	secondFixture.transactionID = newApplicationUUID()
	secondFixture.idempotencyKey = "different-idem-" + fixture.suffix
	second := newApplicationWagerWithHash(
		t, secondFixture, domain.WagerKindBet, "25.00", "external-second", "same-body",
	)

	_, err := processor.Process(ctx, second, "corr-second-"+fixture.suffix)
	if !errors.Is(err, ErrExternalTransactionConflict) {
		t.Fatalf("error = %v, want ErrExternalTransactionConflict", err)
	}

	assertApplicationWallet(t, ctx, pool, fixture.walletID, 7500, 2)
	assertApplicationLedgerCount(t, ctx, pool, first.ID(), 1)
	assertApplicationLedgerCount(t, ctx, pool, second.ID(), 0)
}

func TestWagerProcessorRejectedResultReplaysDeterministicallyIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWagerWithHash(
		t, fixture, domain.WagerKindBet, "120.00", "rejected-first", "rejected-payload",
	)
	firstResult, err := processor.Process(ctx, first, "corr-rejected-"+fixture.suffix)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if firstResult.Transaction.Status() != domain.WagerStatusRejected {
		t.Fatalf("status = %s, want REJECTED", firstResult.Transaction.Status())
	}

	replayFixture := fixture
	replayFixture.transactionID = newApplicationUUID()
	replay := newApplicationWagerWithHash(
		t, replayFixture, domain.WagerKindBet, "120.00", "rejected-replay", "rejected-payload",
	)
	replayResult, err := processor.Process(ctx, replay, "corr-rejected-replay-"+fixture.suffix)
	if err != nil {
		t.Fatalf("replay Process() error = %v", err)
	}
	if !replayResult.Replay {
		t.Fatal("rejected request should replay")
	}
	if replayResult.Transaction.ID() != first.ID() {
		t.Fatalf("replayed transaction = %s, want %s", replayResult.Transaction.ID(), first.ID())
	}
	if replayResult.Transaction.FailureCode() != FailureCodeInsufficientFunds {
		t.Fatalf("failure code = %q", replayResult.Transaction.FailureCode())
	}
	if replayResult.Balance.MinorUnits() != 10000 {
		t.Fatalf("replayed balance = %d, want 10000", replayResult.Balance.MinorUnits())
	}

	assertApplicationWallet(t, ctx, pool, fixture.walletID, 10000, 1)
	assertApplicationLedgerCount(t, ctx, pool, first.ID(), 0)
	assertApplicationOutboxCount(t, ctx, pool, first.ID(), 1)
}

func TestWagerProcessorPendingReferenceReplaysDeterministicallyIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	isolatePendingReferenceWorkerTest(t, ctx, pool)
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	first := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"pending-idem", "missing-idem-ref-"+fixture.suffix,
	)
	firstResult, err := processor.Process(ctx, first, "corr-pending-idem-"+fixture.suffix)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if firstResult.Transaction.Status() != domain.WagerStatusPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", firstResult.Transaction.Status())
	}

	replayFixture := fixture
	replayFixture.transactionID = newApplicationUUID()
	replay := newReferencedApplicationWager(
		t, replayFixture, domain.WagerKindRefund, "25.00",
		"pending-idem", "missing-idem-ref-"+fixture.suffix,
	)
	replayResult, err := processor.Process(ctx, replay, "corr-pending-idem-replay-"+fixture.suffix)
	if err != nil {
		t.Fatalf("replay Process() error = %v", err)
	}
	if !replayResult.Replay {
		t.Fatal("pending reference request should replay")
	}
	if replayResult.Transaction.ID() != first.ID() {
		t.Fatalf("replayed transaction = %s, want %s", replayResult.Transaction.ID(), first.ID())
	}
	if replayResult.Balance.MinorUnits() != 10000 {
		t.Fatalf("replayed balance = %d, want 10000", replayResult.Balance.MinorUnits())
	}
	assertApplicationOutboxCount(t, ctx, pool, first.ID(), 1)
}

func TestWagerProcessorIdempotencyIsIsolatedByProviderIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	firstFixture := newApplicationFixture()
	secondFixture := newApplicationFixture()

	// Do not rely on clock resolution to make back-to-back fixtures distinct.
	// This test specifically requires two different providers and players.
	secondFixture.providerID = "application-provider-two-" + secondFixture.suffix
	secondFixture.playerID = "application-player-provider-two-" + secondFixture.suffix

	sharedIdempotencyKey := "shared-idem-" + firstFixture.suffix
	sharedExternalID := "shared-external-" + firstFixture.suffix
	firstFixture.idempotencyKey = sharedIdempotencyKey
	firstFixture.externalID = sharedExternalID
	secondFixture.idempotencyKey = sharedIdempotencyKey
	secondFixture.externalID = sharedExternalID

	insertApplicationWallet(t, ctx, pool, firstFixture, 10000)
	insertApplicationWallet(t, ctx, pool, secondFixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWagerWithHash(
		t, firstFixture, domain.WagerKindBet, "25.00", "provider-one", "provider-shared-payload",
	)
	second := newApplicationWagerWithHash(
		t, secondFixture, domain.WagerKindBet, "25.00", "provider-two", "provider-shared-payload",
	)

	firstResult, err := processor.Process(ctx, first, "corr-provider-one-"+firstFixture.suffix)
	if err != nil {
		t.Fatalf("first provider Process() error = %v", err)
	}
	secondResult, err := processor.Process(ctx, second, "corr-provider-two-"+secondFixture.suffix)
	if err != nil {
		t.Fatalf("second provider Process() error = %v", err)
	}
	if firstResult.Replay || secondResult.Replay {
		t.Fatal("different providers must not replay each other's transaction")
	}

	assertApplicationWallet(t, ctx, pool, firstFixture.walletID, 7500, 2)
	assertApplicationWallet(t, ctx, pool, secondFixture.walletID, 7500, 2)
}
