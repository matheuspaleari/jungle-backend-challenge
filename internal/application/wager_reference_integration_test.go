package application

import (
	"context"
	"testing"
	"time"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestWagerProcessorRefundCreditsProcessedBetIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	bet := newApplicationWager(t, fixture, domain.WagerKindBet, "25.00", "refund-origin")
	if _, err := processor.Process(ctx, bet, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("process BET: %v", err)
	}

	refundFixture := fixture
	refundFixture.transactionID = newApplicationUUID()
	refundFixture.externalID = "refund-" + fixture.suffix
	refundFixture.idempotencyKey = "refund-idem-" + fixture.suffix

	refund := newReferencedApplicationWager(
		t, refundFixture, domain.WagerKindRefund, "25.00",
		"refund-payload", bet.ExternalTransactionID(),
	)
	result, err := processor.Process(ctx, refund, "corr-refund-"+fixture.suffix)
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}
	if result.Transaction.Status() != domain.WagerStatusProcessed {
		t.Fatalf("status = %s, want PROCESSED", result.Transaction.Status())
	}
	if result.Balance.MinorUnits() != 10000 {
		t.Fatalf("balance = %d, want 10000", result.Balance.MinorUnits())
	}
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 1)
}

func TestWagerProcessorRollbackWinDebitsIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	win := newApplicationWager(t, fixture, domain.WagerKindWin, "25.00", "rollback-win-origin")
	if _, err := processor.Process(ctx, win, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("process WIN: %v", err)
	}

	rollbackFixture := fixture
	rollbackFixture.transactionID = newApplicationUUID()
	rollbackFixture.externalID = "rollback-" + fixture.suffix
	rollbackFixture.idempotencyKey = "rollback-idem-" + fixture.suffix

	rollback := newReferencedApplicationWager(
		t, rollbackFixture, domain.WagerKindRollback, "25.00",
		"rollback-payload", win.ExternalTransactionID(),
	)
	result, err := processor.Process(ctx, rollback, "corr-rollback-"+fixture.suffix)
	if err != nil {
		t.Fatalf("process ROLLBACK: %v", err)
	}
	if result.Transaction.Status() != domain.WagerStatusProcessed {
		t.Fatalf("status = %s, want PROCESSED", result.Transaction.Status())
	}
	if result.Balance.MinorUnits() != 10000 {
		t.Fatalf("balance = %d, want 10000", result.Balance.MinorUnits())
	}
	assertApplicationLedgerCount(t, ctx, pool, rollback.ID(), 1)
}

func TestWagerProcessorMissingReferenceBecomesPendingIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	refund := newReferencedApplicationWager(
		t, fixture, domain.WagerKindRefund, "25.00",
		"missing-ref", "missing-external-"+fixture.suffix,
	)
	result, err := processor.Process(ctx, refund, "corr-pending-"+fixture.suffix)
	if err != nil {
		t.Fatalf("process pending REFUND: %v", err)
	}
	if result.Transaction.Status() != domain.WagerStatusPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Transaction.Status())
	}

	var (
		state      string
		attempts   int
		nextNil    bool
		expiresNil bool
	)
	err = pool.QueryRow(ctx, `
		SELECT state, reference_attempt_count,
		       reference_next_attempt_at IS NULL,
		       reference_expires_at IS NULL
		FROM wager_transactions WHERE id = $1
	`, refund.ID()).Scan(&state, &attempts, &nextNil, &expiresNil)
	if err != nil {
		t.Fatalf("query pending reference: %v", err)
	}
	if state != "PENDING_REFERENCE" || attempts != 0 || nextNil || expiresNil {
		t.Fatalf("invalid pending reference persistence: state=%s attempts=%d nextNil=%v expiresNil=%v",
			state, attempts, nextNil, expiresNil)
	}
	assertApplicationLedgerCount(t, ctx, pool, refund.ID(), 0)
	assertApplicationOutboxCount(t, ctx, pool, refund.ID(), 1)
}

func TestWagerProcessorRejectsSecondSuccessfulReversalIntegration(t *testing.T) {
	pool := newApplicationIntegrationPool(t)
	ctx := context.Background()
	fixture := newApplicationFixture()
	insertApplicationWallet(t, ctx, pool, fixture, 10000)
	processor := newIntegrationWagerProcessor(t, pool)

	bet := newApplicationWager(t, fixture, domain.WagerKindBet, "25.00", "double-reversal-origin")
	if _, err := processor.Process(ctx, bet, "corr-"+fixture.suffix); err != nil {
		t.Fatalf("process BET: %v", err)
	}

	firstFixture := fixture
	firstFixture.transactionID = newApplicationUUID()
	firstFixture.externalID = "refund-first-" + fixture.suffix
	firstFixture.idempotencyKey = "refund-first-idem-" + fixture.suffix
	first := newReferencedApplicationWager(
		t, firstFixture, domain.WagerKindRefund, "25.00",
		"refund-first", bet.ExternalTransactionID(),
	)
	if _, err := processor.Process(ctx, first, "corr-first-"+fixture.suffix); err != nil {
		t.Fatalf("process first reversal: %v", err)
	}

	secondFixture := fixture
	secondFixture.transactionID = newApplicationUUID()
	secondFixture.externalID = "rollback-second-" + fixture.suffix
	secondFixture.idempotencyKey = "rollback-second-idem-" + fixture.suffix
	second := newReferencedApplicationWager(
		t, secondFixture, domain.WagerKindRollback, "25.00",
		"rollback-second", bet.ExternalTransactionID(),
	)
	result, err := processor.Process(ctx, second, "corr-second-"+fixture.suffix)
	if err != nil {
		t.Fatalf("process second reversal: %v", err)
	}
	if result.Transaction.Status() != domain.WagerStatusRejected {
		t.Fatalf("status = %s, want REJECTED", result.Transaction.Status())
	}
	if result.Transaction.FailureCode() != FailureCodeAlreadyReversed {
		t.Fatalf("failure = %q, want %q", result.Transaction.FailureCode(), FailureCodeAlreadyReversed)
	}
	assertApplicationLedgerCount(t, ctx, pool, second.ID(), 0)
}

func newReferencedApplicationWager(
	t *testing.T,
	fixture applicationFixture,
	kind domain.WagerKind,
	amount string,
	hash string,
	referenceExternalID string,
) *domain.WagerTransaction {
	t.Helper()

	money, err := domain.NewMoney(amount, "BRL")
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}

	transaction, err := domain.NewExternalWagerTransaction(
		domain.NewExternalWagerTransactionParams{
			ID:                    fixture.transactionID,
			ExternalTransactionID: fixture.externalID,
			ProviderID:            fixture.providerID,
			IdempotencyKey:        fixture.idempotencyKey,
			PayloadHash:           "payload-" + hash,
			WalletID:              fixture.walletID,
			PlayerID:              fixture.playerID,
			RoundID:               fixture.roundID,
			GameID:                "application-game",
			Kind:                  kind,
			Money:                 money,
			ReferenceExternalID:   referenceExternalID,
			CreatedAt:             time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf("NewExternalWagerTransaction() error = %v", err)
	}
	return transaction
}
