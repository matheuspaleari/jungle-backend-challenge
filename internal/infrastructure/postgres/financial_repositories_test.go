package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestWagerRepositoryInsertAndLookupIntegration(t *testing.T) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()
	prepareFinancialRepositoryWallet(t, ctx, pool, fixture)

	repository, err := NewWagerRepository(pool)
	if err != nil {
		t.Fatalf("NewWagerRepository() error = %v", err)
	}

	money, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	now := time.Now().UTC()

	transaction, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           "payload-hash-001",
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "game-001",
			Kind:                  domain.WagerKindBet,
			Money:                 money,
			CreatedAt:             now,
		},
	)
	if err != nil {
		t.Fatalf("NewExternalWagerTransaction() error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, transaction); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Insert() error = %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	byID, err := repository.GetByID(
		ctx,
		fixture.transactionID,
	)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if byID.ID() != fixture.transactionID {
		t.Fatalf(
			"ID = %q, want %q",
			byID.ID(),
			fixture.transactionID,
		)
	}

	if byID.Money().MinorUnits() != 2500 {
		t.Fatalf(
			"amount = %d, want 2500",
			byID.Money().MinorUnits(),
		)
	}

	if byID.Status() != domain.WagerStatusPending {
		t.Fatalf(
			"status = %q, want PENDING",
			byID.Status(),
		)
	}

	byIdempotency, err := repository.GetByIdempotencyKey(
		ctx,
		fixture.providerID,
		fixture.idempotencyKey,
	)
	if err != nil {
		t.Fatalf(
			"GetByIdempotencyKey() error = %v",
			err,
		)
	}

	if byIdempotency.ID() != fixture.transactionID {
		t.Fatalf(
			"idempotency lookup ID = %q, want %q",
			byIdempotency.ID(),
			fixture.transactionID,
		)
	}

	byExternal, err := repository.GetByProviderAndExternalID(
		ctx,
		fixture.providerID,
		fixture.externalID,
	)
	if err != nil {
		t.Fatalf(
			"GetByProviderAndExternalID() error = %v",
			err,
		)
	}

	if byExternal.ID() != fixture.transactionID {
		t.Fatalf(
			"external lookup ID = %q, want %q",
			byExternal.ID(),
			fixture.transactionID,
		)
	}
}

func TestWagerRepositoryPersistsOriginalResultBalanceIntegration(
	t *testing.T,
) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()
	prepareFinancialRepositoryWallet(t, ctx, pool, fixture)

	repository, err := NewWagerRepository(pool)
	if err != nil {
		t.Fatalf("NewWagerRepository() error = %v", err)
	}

	money, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	resultBalance, err := domain.NewMoney("75.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(result) error = %v", err)
	}

	now := time.Now().UTC()

	transaction, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           "payload-hash-002",
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "game-001",
			Kind:                  domain.WagerKindBet,
			Money:                 money,
			CreatedAt:             now,
		},
	)
	if err != nil {
		t.Fatalf("NewExternalWagerTransaction() error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin insert transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, transaction); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Insert() error = %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit insert: %v", err)
	}

	if err := transaction.SetResultBalance(resultBalance); err != nil {
		t.Fatalf("SetResultBalance() error = %v", err)
	}

	if err := transaction.MarkProcessed(
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("MarkProcessed() error = %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin update transaction: %v", err)
	}

	if err := repository.Update(ctx, tx, transaction); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Update() error = %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit update: %v", err)
	}

	persisted, err := repository.GetByID(
		ctx,
		fixture.transactionID,
	)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if persisted.Status() != domain.WagerStatusProcessed {
		t.Fatalf(
			"status = %q, want PROCESSED",
			persisted.Status(),
		)
	}

	balance, ok := persisted.ResultBalance()
	if !ok {
		t.Fatal("persisted result balance not found")
	}

	if balance.MinorUnits() != 7500 {
		t.Fatalf(
			"result balance = %d, want 7500",
			balance.MinorUnits(),
		)
	}
}

func TestWagerRepositoryPersistentIdempotencyIntegration(t *testing.T) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()
	prepareFinancialRepositoryWallet(t, ctx, pool, fixture)

	repository, err := NewWagerRepository(pool)
	if err != nil {
		t.Fatalf("NewWagerRepository() error = %v", err)
	}

	money, err := domain.NewMoney("10.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	first, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           "hash-a",
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "game-001",
			Kind:                  domain.WagerKindBet,
			Money:                 money,
			CreatedAt:             time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf("create first wager: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, first); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert first wager: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first wager: %v", err)
	}

	second, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    newTestUUID(),
			ExternalTransactionID: fixture.externalID + "-different",
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           "hash-b",
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "game-001",
			Kind:                  domain.WagerKindBet,
			Money:                 money,
			CreatedAt:             time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf("create second wager: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin duplicate transaction: %v", err)
	}

	err = repository.Insert(ctx, tx, second)
	if err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("duplicate idempotency key was accepted")
	}

	_ = tx.Rollback(ctx)
}

func TestLedgerRepositoryInsertIntegration(t *testing.T) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()
	prepareFinancialRepositoryWallet(t, ctx, pool, fixture)
	prepareFinancialRepositoryWager(t, ctx, pool, fixture)

	value, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(value) error = %v", err)
	}

	before, err := domain.NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(before) error = %v", err)
	}

	after, err := domain.NewMoney("75.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(after) error = %v", err)
	}

	entry, err := domain.NewLedgerEntry(
		fixture.ledgerID,
		fixture.walletID,
		fixture.transactionID,
		domain.LedgerDirectionDebit,
		value,
		before,
		after,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("NewLedgerEntry() error = %v", err)
	}

	repository := NewLedgerRepository()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, entry); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Insert() error = %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	var (
		direction string
		valueDB   int64
		beforeDB  int64
		afterDB   int64
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				direction,
				value_minor,
				balance_before_minor,
				balance_after_minor
			FROM ledger_entries
			WHERE id = $1
		`,
		fixture.ledgerID,
	).Scan(
		&direction,
		&valueDB,
		&beforeDB,
		&afterDB,
	)
	if err != nil {
		t.Fatalf("query ledger entry: %v", err)
	}

	if direction != "DEBIT" ||
		valueDB != 2500 ||
		beforeDB != 10000 ||
		afterDB != 7500 {
		t.Fatalf(
			"unexpected ledger values: %s %d %d %d",
			direction,
			valueDB,
			beforeDB,
			afterDB,
		)
	}
}

func TestLedgerRepositoryRejectsDuplicateMovementIntegration(t *testing.T) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()
	prepareFinancialRepositoryWallet(t, ctx, pool, fixture)
	prepareFinancialRepositoryWager(t, ctx, pool, fixture)

	value, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(value) error = %v", err)
	}

	before, err := domain.NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(before) error = %v", err)
	}

	after, err := domain.NewMoney("75.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney(after) error = %v", err)
	}

	first, err := domain.NewLedgerEntry(
		fixture.ledgerID,
		fixture.walletID,
		fixture.transactionID,
		domain.LedgerDirectionDebit,
		value,
		before,
		after,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("create first ledger entry: %v", err)
	}

	second, err := domain.NewLedgerEntry(
		newTestUUID(),
		fixture.walletID,
		fixture.transactionID,
		domain.LedgerDirectionDebit,
		value,
		before,
		after,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("create second ledger entry: %v", err)
	}

	repository := NewLedgerRepository()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, first); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert first ledger entry: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first ledger entry: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin duplicate transaction: %v", err)
	}

	err = repository.Insert(ctx, tx, second)
	if err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("duplicate wallet/transaction movement was accepted")
	}

	_ = tx.Rollback(ctx)
}

func TestOutboxRepositoryInsertIntegration(t *testing.T) {
	pool := newFinancialRepositoryIntegrationPool(t)
	ctx := context.Background()

	fixture := newFinancialFixture()

	money, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	now := time.Now().UTC()

	event, err := domain.NewEvent(
		fixture.eventID,
		domain.EventTypeWagerTransactionProcessed,
		fixture.transactionID,
		"correlation-"+fixture.suffix,
		"",
		now,
		1,
		domain.WagerTransactionProcessedData{
			TransactionID:         fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			Kind:                  domain.WagerKindBet,
			Money:                 domain.NewMoneySnapshot(money),
		},
	)
	if err != nil {
		t.Fatalf("NewEvent() error = %v", err)
	}

	repository := NewOutboxRepository()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	if err := repository.Insert(ctx, tx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Insert() error = %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	var (
		eventType     string
		eventVersion  int64
		payload       []byte
		retryCount    int
		publishedAt   *time.Time
		nextAttemptAt time.Time
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				event_type,
				event_version,
				payload,
				retry_count,
				published_at,
				next_attempt_at
			FROM outbox_events
			WHERE id = $1
		`,
		fixture.eventID,
	).Scan(
		&eventType,
		&eventVersion,
		&payload,
		&retryCount,
		&publishedAt,
		&nextAttemptAt,
	)
	if err != nil {
		t.Fatalf("query outbox event: %v", err)
	}

	if eventType != string(domain.EventTypeWagerTransactionProcessed) {
		t.Fatalf("event type = %q", eventType)
	}

	if eventVersion != 1 {
		t.Fatalf(
			"event version = %d, want 1",
			eventVersion,
		)
	}

	if retryCount != 0 {
		t.Fatalf(
			"retry count = %d, want 0",
			retryCount,
		)
	}

	if publishedAt != nil {
		t.Fatal("new outbox event unexpectedly published")
	}

	expectedNextAttemptAt := now.Truncate(time.Microsecond)

	if !nextAttemptAt.Equal(expectedNextAttemptAt) {
		t.Fatalf(
			"next attempt = %v, want %v",
			nextAttemptAt,
			expectedNextAttemptAt,
		)
	}

	var decoded map[string]any

	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	moneyPayload, ok := decoded["money"].(map[string]any)
	if !ok {
		t.Fatal("money payload missing")
	}

	if moneyPayload["amount"] != "25.00" {
		t.Fatalf(
			"money amount = %v, want 25.00",
			moneyPayload["amount"],
		)
	}

	if moneyPayload["currency"] != "BRL" {
		t.Fatalf(
			"money currency = %v, want BRL",
			moneyPayload["currency"],
		)
	}
}

func TestRepositoriesRequireTransaction(t *testing.T) {
	ctx := context.Background()

	ledgerRepository := NewLedgerRepository()
	if !errors.Is(
		ledgerRepository.Insert(ctx, nil, nil),
		ErrLedgerRepositoryTxRequired,
	) {
		t.Fatal("ledger repository should require transaction")
	}

	outboxRepository := NewOutboxRepository()
	if !errors.Is(
		outboxRepository.Insert(ctx, nil, nil),
		ErrOutboxRepositoryTxRequired,
	) {
		t.Fatal("outbox repository should require transaction")
	}
}

type financialFixture struct {
	suffix         string
	walletID       string
	transactionID  string
	ledgerID       string
	eventID        string
	playerID       string
	providerID     string
	externalID     string
	idempotencyKey string
	roundID        string
}

func newFinancialFixture() financialFixture {
	suffix := fmt.Sprintf(
		"%d",
		time.Now().UTC().UnixNano(),
	)

	return financialFixture{
		suffix:         suffix,
		walletID:       newTestUUID(),
		transactionID:  newTestUUID(),
		ledgerID:       newTestUUID(),
		eventID:        newTestUUID(),
		playerID:       "financial-player-" + suffix,
		providerID:     "provider-" + suffix,
		externalID:     "external-" + suffix,
		idempotencyKey: "idempotency-" + suffix,
		roundID:        "round-" + suffix,
	}
}

func newTestUUID() string {
	now := time.Now().UTC().UnixNano()

	return fmt.Sprintf(
		"90000000-0000-4000-8000-%012x",
		uint64(now)&0xffffffffffff,
	)
}

func prepareFinancialRepositoryWallet(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture financialFixture,
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
				10000,
				1,
				NOW(),
				NOW()
			)
		`,
		fixture.walletID,
		fixture.playerID,
	)
	if err != nil {
		t.Fatalf("insert financial test wallet: %v", err)
	}
}

func prepareFinancialRepositoryWager(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture financialFixture,
) {
	t.Helper()

	_, err := pool.Exec(
		ctx,
		`
			INSERT INTO wager_transactions (
				id,
				external_transaction_id,
				provider_id,
				idempotency_key,
				payload_hash,
				wallet_id,
				player_id,
				round_id,
				game_id,
				kind,
				amount_minor,
				currency,
				state,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				'ledger-hash',
				$5,
				$6,
				$7,
				'game-001',
				'BET',
				2500,
				'BRL',
				'PENDING',
				NOW(),
				NOW()
			)
		`,
		fixture.transactionID,
		fixture.externalID,
		fixture.providerID,
		fixture.idempotencyKey,
		fixture.walletID,
		fixture.playerID,
		fixture.roundID,
	)
	if err != nil {
		t.Fatalf("insert financial test wager: %v", err)
	}
}

func newFinancialRepositoryIntegrationPool(
	t *testing.T,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	pool, err := NewPool(
		context.Background(),
		databaseURL,
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}
