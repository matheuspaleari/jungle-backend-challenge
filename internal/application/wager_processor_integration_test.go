package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

func TestWagerProcessorBetAtomicIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	transaction := newApplicationWager(
		t,
		fixture,
		domain.WagerKindBet,
		"25.00",
		"bet-atomic",
	)

	result, err := processor.Process(
		ctx,
		transaction,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if result.Replay {
		t.Fatal("first request unexpectedly marked as replay")
	}

	if result.Balance.MinorUnits() != 7500 {
		t.Fatalf(
			"balance = %d, want 7500",
			result.Balance.MinorUnits(),
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		7500,
		2,
	)

	assertApplicationWager(
		t,
		ctx,
		pool,
		transaction.ID(),
		"PROCESSED",
		7500,
	)

	assertApplicationLedgerCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		1,
	)

	assertApplicationOutboxCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		2,
	)
}

func TestWagerProcessorWinAtomicIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	transaction := newApplicationWager(
		t,
		fixture,
		domain.WagerKindWin,
		"25.00",
		"win-atomic",
	)

	result, err := processor.Process(
		ctx,
		transaction,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if result.Balance.MinorUnits() != 12500 {
		t.Fatalf(
			"balance = %d, want 12500",
			result.Balance.MinorUnits(),
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		12500,
		2,
	)

	assertApplicationLedgerCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		1,
	)

	assertApplicationOutboxCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		2,
	)
}

func TestWagerProcessorLossDoesNotMoveBalanceIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	transaction := newApplicationWager(
		t,
		fixture,
		domain.WagerKindLoss,
		"0.00",
		"loss-no-movement",
	)

	result, err := processor.Process(
		ctx,
		transaction,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if result.Balance.MinorUnits() != 10000 {
		t.Fatalf(
			"balance = %d, want 10000",
			result.Balance.MinorUnits(),
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		10000,
		1,
	)

	assertApplicationLedgerCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		0,
	)

	assertApplicationOutboxCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		1,
	)
}

func TestWagerProcessorInsufficientFundsIsDurableRejectionIntegration(
	t *testing.T,
) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	transaction := newApplicationWager(
		t,
		fixture,
		domain.WagerKindBet,
		"120.00",
		"bet-insufficient",
	)

	result, err := processor.Process(
		ctx,
		transaction,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if result.Transaction.Status() != domain.WagerStatusRejected {
		t.Fatalf(
			"status = %s, want REJECTED",
			result.Transaction.Status(),
		)
	}

	if result.Transaction.FailureCode() != FailureCodeInsufficientFunds {
		t.Fatalf(
			"failure code = %q",
			result.Transaction.FailureCode(),
		)
	}

	if result.Balance.MinorUnits() != 10000 {
		t.Fatalf(
			"balance = %d, want 10000",
			result.Balance.MinorUnits(),
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		10000,
		1,
	)

	assertApplicationLedgerCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		0,
	)

	assertApplicationOutboxCount(
		t,
		ctx,
		pool,
		transaction.ID(),
		1,
	)
}

func TestWagerProcessorReplayDoesNotApplyMovementAgainIntegration(
	t *testing.T,
) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWager(
		t,
		fixture,
		domain.WagerKindBet,
		"25.00",
		"replay",
	)

	firstResult, err := processor.Process(
		ctx,
		first,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}

	if firstResult.Replay {
		t.Fatal("first request unexpectedly replay")
	}

	second := newApplicationWager(
		t,
		fixture,
		domain.WagerKindBet,
		"25.00",
		"replay",
	)

	secondResult, err := processor.Process(
		ctx,
		second,
		"correlation-"+fixture.suffix,
	)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}

	if !secondResult.Replay {
		t.Fatal("second request should be replay")
	}

	if secondResult.Balance.MinorUnits() != 7500 {
		t.Fatalf(
			"replayed balance = %d, want 7500",
			secondResult.Balance.MinorUnits(),
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		7500,
		2,
	)

	assertApplicationLedgerCount(
		t,
		ctx,
		pool,
		first.ID(),
		1,
	)

	assertApplicationOutboxCount(
		t,
		ctx,
		pool,
		first.ID(),
		2,
	)
}

func TestWagerProcessorIdempotencyConflictIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	first := newApplicationWager(
		t,
		fixture,
		domain.WagerKindBet,
		"25.00",
		"conflict",
	)

	if _, err := processor.Process(
		ctx,
		first,
		"correlation-"+fixture.suffix,
	); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}

	second := newApplicationWagerWithHash(
		t,
		fixture,
		domain.WagerKindBet,
		"25.00",
		"conflict",
		"different-payload-hash",
	)

	_, err := processor.Process(
		ctx,
		second,
		"correlation-"+fixture.suffix,
	)

	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf(
			"error = %v, want ErrIdempotencyConflict",
			err,
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		7500,
		2,
	)
}

func TestWagerProcessorConcurrentBetsAllowOnlyOneIntegration(
	t *testing.T,
) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()

	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)

	processor := newIntegrationWagerProcessor(t, pool)

	firstFixture := fixture
	firstFixture.transactionID = newApplicationUUID()
	firstFixture.externalID = "external-a-" + fixture.suffix
	firstFixture.idempotencyKey = "idem-a-" + fixture.suffix

	secondFixture := fixture
	secondFixture.transactionID = newApplicationUUID()
	secondFixture.externalID = "external-b-" + fixture.suffix
	secondFixture.idempotencyKey = "idem-b-" + fixture.suffix

	first := newApplicationWager(
		t,
		firstFixture,
		domain.WagerKindBet,
		"80.00",
		"concurrent-a",
	)

	second := newApplicationWager(
		t,
		secondFixture,
		domain.WagerKindBet,
		"80.00",
		"concurrent-b",
	)

	type processResult struct {
		result *ProcessWagerResult
		err    error
	}

	start := make(chan struct{})
	results := make(chan processResult, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	run := func(transaction *domain.WagerTransaction) {
		defer wg.Done()

		<-start

		result, err := processor.Process(
			ctx,
			transaction,
			"correlation-"+fixture.suffix,
		)

		results <- processResult{
			result: result,
			err:    err,
		}
	}

	go run(first)
	go run(second)

	close(start)

	wg.Wait()
	close(results)

	processed := 0
	rejected := 0

	for item := range results {
		if item.err != nil {
			t.Fatalf(
				"concurrent Process() error = %v",
				item.err,
			)
		}

		switch item.result.Transaction.Status() {
		case domain.WagerStatusProcessed:
			processed++

		case domain.WagerStatusRejected:
			if item.result.Transaction.FailureCode() !=
				FailureCodeInsufficientFunds {
				t.Fatalf(
					"unexpected failure code = %q",
					item.result.Transaction.FailureCode(),
				)
			}

			rejected++

		default:
			t.Fatalf(
				"unexpected status = %s",
				item.result.Transaction.Status(),
			)
		}
	}

	if processed != 1 {
		t.Fatalf(
			"processed = %d, want 1",
			processed,
		)
	}

	if rejected != 1 {
		t.Fatalf(
			"rejected = %d, want 1",
			rejected,
		)
	}

	assertApplicationWallet(
		t,
		ctx,
		pool,
		fixture.walletID,
		2000,
		2,
	)

	var ledgerCount int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ledger_entries
			WHERE wallet_id = $1
		`,
		fixture.walletID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 1 {
		t.Fatalf(
			"ledger count = %d, want 1",
			ledgerCount,
		)
	}
}

func newIntegrationWagerProcessor(
	t *testing.T,
	pool *pgxpool.Pool,
) *WagerProcessor {
	t.Helper()

	transactionManager, err :=
		postgresinfra.NewTransactionManager(pool)
	if err != nil {
		t.Fatalf(
			"NewTransactionManager() error = %v",
			err,
		)
	}

	walletRepository, err :=
		postgresinfra.NewWalletRepository(pool)
	if err != nil {
		t.Fatalf(
			"NewWalletRepository() error = %v",
			err,
		)
	}

	wagerRepository, err :=
		postgresinfra.NewWagerRepository(pool)
	if err != nil {
		t.Fatalf(
			"NewWagerRepository() error = %v",
			err,
		)
	}

	ledgerRepository :=
		postgresinfra.NewLedgerRepository()

	outboxRepository :=
		postgresinfra.NewOutboxRepository()

	return mustNewWagerProcessor(
		t,
		transactionManager,
		walletRepository,
		wagerRepository,
		ledgerRepository,
		outboxRepository,
	)
}

func mustNewWagerProcessor(
	t *testing.T,
	transactions *postgresinfra.TransactionManager,
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	outbox *postgresinfra.OutboxRepository,
) *WagerProcessor {
	t.Helper()

	processor, err := NewWagerProcessor(
		transactions,
		wallets,
		wagers,
		ledger,
		outbox,
		newApplicationUUID,
		func() time.Time {
			return time.Now().UTC()
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWagerProcessor() error = %v",
			err,
		)
	}

	return processor
}

type applicationFixture struct {
	suffix         string
	walletID       string
	transactionID  string
	playerID       string
	providerID     string
	externalID     string
	idempotencyKey string
	roundID        string
}

func newApplicationFixture() applicationFixture {
	suffix := fmt.Sprintf(
		"%d",
		time.Now().UTC().UnixNano(),
	)

	return applicationFixture{
		suffix:         suffix,
		walletID:       newApplicationUUID(),
		transactionID:  newApplicationUUID(),
		playerID:       "application-player-" + suffix,
		providerID:     "application-provider-" + suffix,
		externalID:     "application-external-" + suffix,
		idempotencyKey: "application-idem-" + suffix,
		roundID:        "application-round-" + suffix,
	}
}

func newApplicationWager(
	t *testing.T,
	fixture applicationFixture,
	kind domain.WagerKind,
	amount string,
	hashSuffix string,
) *domain.WagerTransaction {
	t.Helper()

	return newApplicationWagerWithHash(
		t,
		fixture,
		kind,
		amount,
		hashSuffix,
		"payload-"+hashSuffix,
	)
}

func newApplicationWagerWithHash(
	t *testing.T,
	fixture applicationFixture,
	kind domain.WagerKind,
	amount string,
	hashSuffix string,
	payloadHash string,
) *domain.WagerTransaction {
	t.Helper()

	money, err := domain.NewMoney(
		amount,
		"BRL",
	)
	if err != nil {
		t.Fatalf(
			"NewMoney(%s) error = %v",
			amount,
			err,
		)
	}

	transaction, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           payloadHash,
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "application-game",
			Kind:                  kind,
			Money:                 money,
			CreatedAt:             time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"NewExternalWagerTransaction(%s) error = %v",
			hashSuffix,
			err,
		)
	}

	return transaction
}

func insertApplicationWallet(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture applicationFixture,
	balanceMinor int64,
) {
	t.Helper()

	_, err := pool.Exec(
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
				'BRL',
				$3,
				1,
				NOW(),
				NOW()
			)
		`,
		fixture.walletID,
		fixture.playerID,
		balanceMinor,
	)
	if err != nil {
		t.Fatalf(
			"insert application wallet: %v",
			err,
		)
	}
}

func assertApplicationWallet(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	walletID string,
	expectedBalance int64,
	expectedVersion int64,
) {
	t.Helper()

	var (
		balance int64
		version int64
	)

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				balance_minor,
				version
			FROM wallets
			WHERE id = $1
		`,
		walletID,
	).Scan(
		&balance,
		&version,
	)
	if err != nil {
		t.Fatalf(
			"query wallet: %v",
			err,
		)
	}

	if balance != expectedBalance {
		t.Fatalf(
			"wallet balance = %d, want %d",
			balance,
			expectedBalance,
		)
	}

	if version != expectedVersion {
		t.Fatalf(
			"wallet version = %d, want %d",
			version,
			expectedVersion,
		)
	}
}

func assertApplicationWager(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactionID string,
	expectedState string,
	expectedResultBalance int64,
) {
	t.Helper()

	var (
		state         string
		resultBalance *int64
	)

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				state,
				result_balance_minor
			FROM wager_transactions
			WHERE id = $1
		`,
		transactionID,
	).Scan(
		&state,
		&resultBalance,
	)
	if err != nil {
		t.Fatalf(
			"query wager: %v",
			err,
		)
	}

	if state != expectedState {
		t.Fatalf(
			"wager state = %q, want %q",
			state,
			expectedState,
		)
	}

	if resultBalance == nil {
		t.Fatal("wager result balance is NULL")
	}

	if *resultBalance != expectedResultBalance {
		t.Fatalf(
			"wager result balance = %d, want %d",
			*resultBalance,
			expectedResultBalance,
		)
	}
}

func assertApplicationLedgerCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactionID string,
	expected int,
) {
	t.Helper()

	var count int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ledger_entries
			WHERE transaction_id = $1
		`,
		transactionID,
	).Scan(&count)
	if err != nil {
		t.Fatalf(
			"count ledger entries: %v",
			err,
		)
	}

	if count != expected {
		t.Fatalf(
			"ledger count = %d, want %d",
			count,
			expected,
		)
	}
}

func assertApplicationOutboxCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactionID string,
	expected int,
) {
	t.Helper()

	var count int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE
				aggregate_id = $1
				OR payload->>'transactionId' = $1
		`,
		transactionID,
	).Scan(&count)
	if err != nil {
		t.Fatalf(
			"count outbox events: %v",
			err,
		)
	}

	if count != expected {
		t.Fatalf(
			"outbox count = %d, want %d",
			count,
			expected,
		)
	}
}

func newApplicationIntegrationPool(
	t *testing.T,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv(
		"TEST_DATABASE_URL",
	)
	if databaseURL == "" {
		t.Skip(
			"TEST_DATABASE_URL not configured",
		)
	}

	pool, err := postgresinfra.NewPool(
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

var applicationUUIDCounter uint64

func newApplicationUUID() string {
	counter := atomic.AddUint64(
		&applicationUUIDCounter,
		1,
	)

	now := uint64(
		time.Now().UTC().UnixNano(),
	)

	value := (now ^ counter) &
		0xffffffffffff

	return fmt.Sprintf(
		"a0000000-0000-4000-8000-%012x",
		value,
	)
}
