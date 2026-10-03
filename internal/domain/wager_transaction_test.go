package domain

import (
	"errors"
	"testing"
	"time"
)

func validWagerParams(
	kind WagerKind,
	money Money,
) NewExternalWagerTransactionParams {
	return NewExternalWagerTransactionParams{
		ID:                    "tx-internal-1",
		ExternalTransactionID: "tx-external-1",
		ProviderID:            "provider-a",
		IdempotencyKey:        "provider-a:tx-external-1",
		PayloadHash:           "hash-123",
		WalletID:              "wallet-1",
		PlayerID:              "player-1",
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  kind,
		Money:                 money,
		CreatedAt:             walletTestTime(),
	}
}

func TestNewBetStartsPending(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, err := NewExternalWagerTransaction(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusPending {
		t.Fatalf(
			"expected PENDING, got %s",
			tx.Status(),
		)
	}

	if tx.Kind() != WagerKindBet {
		t.Fatalf(
			"expected BET, got %s",
			tx.Kind(),
		)
	}
}

func TestOpeningCannotBeSubmittedExternally(t *testing.T) {
	money, _ := NewMoney("100.00", "BRL")
	params := validWagerParams(WagerKindOpening, money)

	_, err := NewExternalWagerTransaction(params)

	if !errors.Is(err, ErrOpeningExternalNotAllowed) {
		t.Fatalf(
			"expected ErrOpeningExternalNotAllowed, got %v",
			err,
		)
	}
}

func TestLossRequiresZeroAmount(t *testing.T) {
	money, _ := NewMoney("1.00", "BRL")
	params := validWagerParams(WagerKindLoss, money)

	_, err := NewExternalWagerTransaction(params)

	if !errors.Is(err, ErrInvalidLossAmount) {
		t.Fatalf(
			"expected ErrInvalidLossAmount, got %v",
			err,
		)
	}
}

func TestLossAcceptsZeroAmount(t *testing.T) {
	money, _ := NewMoney("0.00", "BRL")
	params := validWagerParams(WagerKindLoss, money)

	tx, err := NewExternalWagerTransaction(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusPending {
		t.Fatalf(
			"expected PENDING, got %s",
			tx.Status(),
		)
	}
}

func TestBetRejectsZeroAmount(t *testing.T) {
	money, _ := NewMoney("0.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	_, err := NewExternalWagerTransaction(params)

	if !errors.Is(err, ErrInvalidExternalWagerAmount) {
		t.Fatalf(
			"expected ErrInvalidExternalWagerAmount, got %v",
			err,
		)
	}
}

func TestRefundRequiresReference(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindRefund, money)

	_, err := NewExternalWagerTransaction(params)

	if !errors.Is(err, ErrReferenceRequired) {
		t.Fatalf(
			"expected ErrReferenceRequired, got %v",
			err,
		)
	}
}

func TestRollbackRequiresReference(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindRollback, money)

	_, err := NewExternalWagerTransaction(params)

	if !errors.Is(err, ErrReferenceRequired) {
		t.Fatalf(
			"expected ErrReferenceRequired, got %v",
			err,
		)
	}
}

func TestRefundCanBecomePendingReference(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindRefund, money)
	params.ReferenceExternalID = "original-bet"

	tx, err := NewExternalWagerTransaction(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := params.CreatedAt.Add(time.Second)

	err = tx.MarkPendingReference(now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusPendingReference {
		t.Fatalf(
			"expected PENDING_REFERENCE, got %s",
			tx.Status(),
		)
	}
}

func TestPendingReferenceMustResolveBeforeProcessing(
	t *testing.T,
) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindRefund, money)
	params.ReferenceExternalID = "original-bet"

	tx, _ := NewExternalWagerTransaction(params)

	pendingAt := params.CreatedAt.Add(time.Second)

	if err := tx.MarkPendingReference(pendingAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := tx.MarkProcessed(
		pendingAt.Add(time.Second),
	)

	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf(
			"expected ErrInvalidStateTransition, got %v",
			err,
		)
	}
}

func TestPendingReferenceCanResolveAndProcess(
	t *testing.T,
) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindRefund, money)
	params.ReferenceExternalID = "original-bet"

	tx, _ := NewExternalWagerTransaction(params)

	pendingAt := params.CreatedAt.Add(time.Second)

	if err := tx.MarkPendingReference(pendingAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolvedAt := pendingAt.Add(time.Second)

	if err := tx.ResolveReference(
		"internal-original-bet",
		resolvedAt,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	processedAt := resolvedAt.Add(time.Second)

	if err := tx.MarkProcessed(processedAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusProcessed {
		t.Fatalf(
			"expected PROCESSED, got %s",
			tx.Status(),
		)
	}

	if tx.ResolvedReferenceID() != "internal-original-bet" {
		t.Fatalf(
			"unexpected resolved reference: %s",
			tx.ResolvedReferenceID(),
		)
	}
}

func TestBetCanBeProcessedDirectly(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, _ := NewExternalWagerTransaction(params)

	err := tx.MarkProcessed(
		params.CreatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusProcessed {
		t.Fatalf(
			"expected PROCESSED, got %s",
			tx.Status(),
		)
	}
}

func TestTransactionCanBeRejected(t *testing.T) {
	money, _ := NewMoney("80.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, _ := NewExternalWagerTransaction(params)

	err := tx.MarkRejected(
		"INSUFFICIENT_FUNDS",
		params.CreatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusRejected {
		t.Fatalf(
			"expected REJECTED, got %s",
			tx.Status(),
		)
	}

	if tx.FailureCode() != "INSUFFICIENT_FUNDS" {
		t.Fatalf(
			"unexpected failure code: %s",
			tx.FailureCode(),
		)
	}
}

func TestTransactionCanFailPermanently(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, _ := NewExternalWagerTransaction(params)

	err := tx.MarkFailed(
		"PERMANENT_INFRASTRUCTURE_FAILURE",
		params.CreatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Status() != WagerStatusFailed {
		t.Fatalf(
			"expected FAILED, got %s",
			tx.Status(),
		)
	}
}

func TestTerminalTransactionCannotTransitionAgain(
	t *testing.T,
) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, _ := NewExternalWagerTransaction(params)

	processedAt := params.CreatedAt.Add(time.Second)

	if err := tx.MarkProcessed(processedAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := tx.MarkRejected(
		"SOME_FAILURE",
		processedAt.Add(time.Second),
	)

	if !errors.Is(err, ErrTransactionTerminal) {
		t.Fatalf(
			"expected ErrTransactionTerminal, got %v",
			err,
		)
	}
}

func TestRejectionRequiresFailureCode(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")
	params := validWagerParams(WagerKindBet, money)

	tx, _ := NewExternalWagerTransaction(params)

	err := tx.MarkRejected(
		"",
		params.CreatedAt.Add(time.Second),
	)

	if !errors.Is(err, ErrFailureCodeRequired) {
		t.Fatalf(
			"expected ErrFailureCodeRequired, got %v",
			err,
		)
	}
}

func TestWinMayContainReference(t *testing.T) {
	money, _ := NewMoney("50.00", "BRL")
	params := validWagerParams(WagerKindWin, money)
	params.ReferenceExternalID = "bet-123"

	tx, err := NewExternalWagerTransaction(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.ReferenceExternalID() != "bet-123" {
		t.Fatalf(
			"expected bet-123, got %s",
			tx.ReferenceExternalID(),
		)
	}
}
