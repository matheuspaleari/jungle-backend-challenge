package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestWagerProcessorThreeIndependentClientsPreserveWalletInvariantIntegration(t *testing.T) {
	// Three independently-created pools/processors simulate three application
	// instances with separate connection pools and no shared in-memory state.
	poolA := newApplicationIntegrationPool(t)
	poolB := newApplicationIntegrationPool(t)
	poolC := newApplicationIntegrationPool(t)

	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, poolA, fixture, 10000)

	processorA := newIntegrationWagerProcessor(t, poolA)
	processorB := newIntegrationWagerProcessor(t, poolB)
	processorC := newIntegrationWagerProcessor(t, poolC)

	firstFixture := fixture
	firstFixture.transactionID = newApplicationUUID()
	firstFixture.externalID = "concurrency-a-" + fixture.suffix
	firstFixture.idempotencyKey = "concurrency-idem-a-" + fixture.suffix

	secondFixture := fixture
	secondFixture.transactionID = newApplicationUUID()
	secondFixture.externalID = "concurrency-b-" + fixture.suffix
	secondFixture.idempotencyKey = "concurrency-idem-b-" + fixture.suffix

	first := newApplicationWager(
		t,
		firstFixture,
		domain.WagerKindBet,
		"80.00",
		"concurrency-a",
	)
	second := newApplicationWager(
		t,
		secondFixture,
		domain.WagerKindBet,
		"80.00",
		"concurrency-b",
	)

	type outcome struct {
		result *ProcessWagerResult
		err    error
	}

	start := make(chan struct{})
	outcomes := make(chan outcome, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		result, err := processorA.Process(
			ctx,
			first,
			"corr-concurrency-a-"+fixture.suffix,
		)
		outcomes <- outcome{result: result, err: err}
	}()

	go func() {
		defer wg.Done()
		<-start
		result, err := processorB.Process(
			ctx,
			second,
			"corr-concurrency-b-"+fixture.suffix,
		)
		outcomes <- outcome{result: result, err: err}
	}()

	close(start)
	wg.Wait()
	close(outcomes)

	processed := 0
	rejected := 0
	var processedTransaction *domain.WagerTransaction
	var rejectedTransaction *domain.WagerTransaction

	for item := range outcomes {
		if item.err != nil {
			t.Fatalf("concurrent Process() error = %v", item.err)
		}
		if item.result == nil {
			t.Fatal("concurrent Process() returned nil result")
		}

		switch item.result.Transaction.Status() {
		case domain.WagerStatusProcessed:
			processed++
			processedTransaction = item.result.Transaction

		case domain.WagerStatusRejected:
			if item.result.Transaction.FailureCode() != FailureCodeInsufficientFunds {
				t.Fatalf(
					"rejected failure code = %q, want %q",
					item.result.Transaction.FailureCode(),
					FailureCodeInsufficientFunds,
				)
			}
			rejected++
			rejectedTransaction = item.result.Transaction

		default:
			t.Fatalf(
				"unexpected transaction status = %s",
				item.result.Transaction.Status(),
			)
		}
	}

	if processed != 1 || rejected != 1 {
		t.Fatalf(
			"processed=%d rejected=%d, want 1/1",
			processed,
			rejected,
		)
	}

	assertApplicationWallet(t, ctx, poolC, fixture.walletID, 2000, 2)

	var debitCount int
	if err := poolC.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ledger_entries
			WHERE wallet_id = $1
			  AND direction = 'DEBIT'
		`,
		fixture.walletID,
	).Scan(&debitCount); err != nil {
		t.Fatalf("count wallet debits: %v", err)
	}
	if debitCount != 1 {
		t.Fatalf("wallet debit ledger count = %d, want 1", debitCount)
	}

	if processedTransaction == nil || rejectedTransaction == nil {
		t.Fatal("missing processed or rejected transaction")
	}

	// Replays are intentionally executed by the third independent processor.
	// They must reproduce the durable result without another balance movement.
	for _, original := range []*domain.WagerTransaction{
		processedTransaction,
		rejectedTransaction,
	} {
		replayFixture := fixture
		replayFixture.transactionID = newApplicationUUID()
		replayFixture.externalID = original.ExternalTransactionID()
		replayFixture.idempotencyKey = original.IdempotencyKey()

		replay := newApplicationWagerWithHash(
			t,
			replayFixture,
			domain.WagerKindBet,
			"80.00",
			"concurrency-replay",
			original.PayloadHash(),
		)

		result, err := processorC.Process(
			ctx,
			replay,
			"corr-concurrency-replay-"+fixture.suffix,
		)
		if err != nil {
			t.Fatalf("replay Process() error = %v", err)
		}
		if !result.Replay {
			t.Fatal("replayed concurrent request was not marked as replay")
		}
		if result.Transaction.ID() != original.ID() {
			t.Fatalf(
				"replayed transaction ID = %s, want %s",
				result.Transaction.ID(),
				original.ID(),
			)
		}
	}

	assertApplicationWallet(t, ctx, poolC, fixture.walletID, 2000, 2)

	if err := poolC.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ledger_entries
			WHERE wallet_id = $1
			  AND direction = 'DEBIT'
		`,
		fixture.walletID,
	).Scan(&debitCount); err != nil {
		t.Fatalf("count wallet debits after replay: %v", err)
	}
	if debitCount != 1 {
		t.Fatalf(
			"wallet debit ledger count after replay = %d, want 1",
			debitCount,
		)
	}
}

func TestWagerProcessorDifferentWalletsDoNotShareDatabaseLockIntegration(t *testing.T) {
	poolA := newApplicationIntegrationPool(t)
	poolB := newApplicationIntegrationPool(t)
	poolC := newApplicationIntegrationPool(t)

	ctx := context.Background()

	firstFixture := newApplicationFixture()
	secondFixture := newApplicationFixture()

	// Do not rely on timestamp resolution for fixture uniqueness.
	secondFixture.walletID = newApplicationUUID()
	secondFixture.transactionID = newApplicationUUID()
	secondFixture.playerID = "parallel-player-two-" + secondFixture.suffix
	secondFixture.providerID = "parallel-provider-two-" + secondFixture.suffix
	secondFixture.externalID = "parallel-external-two-" + secondFixture.suffix
	secondFixture.idempotencyKey = "parallel-idem-two-" + secondFixture.suffix
	secondFixture.roundID = "parallel-round-two-" + secondFixture.suffix

	insertApplicationWallet(t, ctx, poolA, firstFixture, 10000)
	insertApplicationWallet(t, ctx, poolA, secondFixture, 10000)

	// Hold wallet A's row lock open from client A.
	lockTx, err := poolA.Begin(ctx)
	if err != nil {
		t.Fatalf("begin wallet lock transaction: %v", err)
	}
	defer func() { _ = lockTx.Rollback(context.Background()) }()

	if _, err := lockTx.Exec(
		ctx,
		`SELECT id FROM wallets WHERE id = $1 FOR UPDATE`,
		firstFixture.walletID,
	); err != nil {
		t.Fatalf("lock first wallet: %v", err)
	}

	processorB := newIntegrationWagerProcessor(t, poolB)
	processorC := newIntegrationWagerProcessor(t, poolC)

	blockedFixture := firstFixture
	blockedFixture.transactionID = newApplicationUUID()
	blockedFixture.externalID = "blocked-wallet-" + firstFixture.suffix
	blockedFixture.idempotencyKey = "blocked-wallet-idem-" + firstFixture.suffix

	blockedWager := newApplicationWager(
		t,
		blockedFixture,
		domain.WagerKindBet,
		"10.00",
		"blocked-wallet",
	)

	independentFixture := secondFixture
	independentFixture.transactionID = newApplicationUUID()
	independentFixture.externalID = "independent-wallet-" + secondFixture.suffix
	independentFixture.idempotencyKey = "independent-wallet-idem-" + secondFixture.suffix

	independentWager := newApplicationWager(
		t,
		independentFixture,
		domain.WagerKindBet,
		"10.00",
		"independent-wallet",
	)

	blockedDone := make(chan error, 1)
	go func() {
		blockedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := processorB.Process(
			blockedCtx,
			blockedWager,
			"corr-blocked-"+firstFixture.suffix,
		)
		blockedDone <- err
	}()

	// Give client B enough time to reach the row lock.
	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-blockedDone:
		t.Fatalf(
			"same-wallet operation finished while wallet lock was held; err=%v",
			err,
		)
	default:
	}

	independentCtx, cancelIndependent := context.WithTimeout(ctx, 2*time.Second)
	defer cancelIndependent()

	independentResult, err := processorC.Process(
		independentCtx,
		independentWager,
		"corr-independent-"+secondFixture.suffix,
	)
	if err != nil {
		t.Fatalf("independent wallet Process() error = %v", err)
	}
	if independentResult.Transaction.Status() != domain.WagerStatusProcessed {
		t.Fatalf(
			"independent wallet status = %s, want PROCESSED",
			independentResult.Transaction.Status(),
		)
	}
	if independentResult.Balance.MinorUnits() != 9000 {
		t.Fatalf(
			"independent wallet balance = %d, want 9000",
			independentResult.Balance.MinorUnits(),
		)
	}

	// Releasing wallet A must allow the blocked operation to continue.
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("commit wallet lock transaction: %v", err)
	}

	select {
	case err := <-blockedDone:
		if err != nil {
			t.Fatalf("blocked wallet Process() error after unlock = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("same-wallet operation did not continue after lock release")
	}

	assertApplicationWallet(t, ctx, poolA, firstFixture.walletID, 9000, 2)
	assertApplicationWallet(t, ctx, poolA, secondFixture.walletID, 9000, 2)
}
