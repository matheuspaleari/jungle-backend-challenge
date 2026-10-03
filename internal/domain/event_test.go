package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	money, _ := NewMoney("80.00", "BRL")

	data := WagerTransactionProcessedData{
		TransactionID:         "transaction-1",
		ExternalTransactionID: "external-1",
		ProviderID:            "provider-a",
		WalletID:              "wallet-1",
		PlayerID:              "player-1",
		Kind:                  WagerKindBet,
		Money:                 NewMoneySnapshot(money),
	}

	event, err := NewEvent(
		"event-1",
		EventTypeWagerTransactionProcessed,
		"transaction-1",
		"correlation-1",
		"",
		walletTestTime(),
		1,
		data,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.EventID() != "event-1" {
		t.Fatalf(
			"expected event-1, got %s",
			event.EventID(),
		)
	}

	if event.EventType() != EventTypeWagerTransactionProcessed {
		t.Fatalf(
			"unexpected event type: %s",
			event.EventType(),
		)
	}

	if event.Version() != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			event.Version(),
		)
	}
}

func TestMoneySnapshotUsesDecimalString(t *testing.T) {
	money, _ := NewMoney("25.50", "BRL")

	snapshot := NewMoneySnapshot(money)

	if snapshot.Amount != "25.50" {
		t.Fatalf(
			"expected 25.50, got %s",
			snapshot.Amount,
		)
	}

	if snapshot.Currency != "BRL" {
		t.Fatalf(
			"expected BRL, got %s",
			snapshot.Currency,
		)
	}
}

func TestWalletBalanceChangedData(t *testing.T) {
	value, _ := NewMoney("80.00", "BRL")
	before, _ := NewMoney("100.00", "BRL")
	after, _ := NewMoney("20.00", "BRL")

	data := WalletBalanceChangedData{
		WalletID:      "wallet-1",
		TransactionID: "transaction-1",
		Direction:     LedgerDirectionDebit,
		Money:         NewMoneySnapshot(value),
		BalanceBefore: NewMoneySnapshot(before),
		BalanceAfter:  NewMoneySnapshot(after),
		WalletVersion: 2,
	}

	if data.Money.Amount != "80.00" {
		t.Fatalf(
			"expected movement 80.00, got %s",
			data.Money.Amount,
		)
	}

	if data.BalanceBefore.Amount != "100.00" {
		t.Fatalf(
			"expected balance before 100.00, got %s",
			data.BalanceBefore.Amount,
		)
	}

	if data.BalanceAfter.Amount != "20.00" {
		t.Fatalf(
			"expected balance after 20.00, got %s",
			data.BalanceAfter.Amount,
		)
	}

	if data.WalletVersion != 2 {
		t.Fatalf(
			"expected wallet version 2, got %d",
			data.WalletVersion,
		)
	}
}

func TestEventRejectsUnknownType(t *testing.T) {
	_, err := NewEvent(
		"event-1",
		EventType("UNKNOWN"),
		"transaction-1",
		"correlation-1",
		"",
		walletTestTime(),
		1,
		struct{}{},
	)

	if !errors.Is(err, ErrInvalidEventType) {
		t.Fatalf(
			"expected ErrInvalidEventType, got %v",
			err,
		)
	}
}

func TestEventRequiresCorrelationID(t *testing.T) {
	_, err := NewEvent(
		"event-1",
		EventTypeWagerTransactionProcessed,
		"transaction-1",
		"",
		"",
		walletTestTime(),
		1,
		struct{}{},
	)

	if !errors.Is(err, ErrInvalidCorrelationID) {
		t.Fatalf(
			"expected ErrInvalidCorrelationID, got %v",
			err,
		)
	}
}

func TestEventRejectsZeroVersion(t *testing.T) {
	_, err := NewEvent(
		"event-1",
		EventTypeWagerTransactionProcessed,
		"transaction-1",
		"correlation-1",
		"",
		walletTestTime(),
		0,
		struct{}{},
	)

	if !errors.Is(err, ErrInvalidEventVersion) {
		t.Fatalf(
			"expected ErrInvalidEventVersion, got %v",
			err,
		)
	}
}

func TestEventTimestampIsConvertedToUTC(t *testing.T) {
	location := time.FixedZone("BRT", -3*60*60)

	occurredAt := time.Date(
		2026,
		time.October,
		2,
		10,
		30,
		0,
		0,
		location,
	)

	event, err := NewEvent(
		"event-1",
		EventTypeWagerTransactionProcessed,
		"transaction-1",
		"correlation-1",
		"",
		occurredAt,
		1,
		struct{}{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.OccurredAt().Location() != time.UTC {
		t.Fatal("expected event timestamp in UTC")
	}

	if event.OccurredAt().Format(time.RFC3339) !=
		"2026-10-02T13:30:00Z" {
		t.Fatalf(
			"unexpected UTC timestamp: %s",
			event.OccurredAt().Format(time.RFC3339),
		)
	}
}

func TestPendingReferenceEventData(t *testing.T) {
	data := PendingReferenceData{
		TransactionID:         "refund-1",
		ExternalTransactionID: "external-refund-1",
		ProviderID:            "provider-a",
		WalletID:              "wallet-1",
		Kind:                  WagerKindRefund,
		ReferenceExternalID:   "original-bet-1",
	}

	event, err := NewEvent(
		"event-1",
		EventTypePendingReference,
		"refund-1",
		"correlation-1",
		"message-1",
		walletTestTime(),
		1,
		data,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.CausationID() != "message-1" {
		t.Fatalf(
			"expected message-1, got %s",
			event.CausationID(),
		)
	}
}
