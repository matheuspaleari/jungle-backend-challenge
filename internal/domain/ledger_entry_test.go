package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewDebitLedgerEntry(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("80.00", "BRL")
	after, _ := NewMoney("20.00", "BRL")

	entry, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		after,
		walletTestTime(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Direction() != LedgerDirectionDebit {
		t.Fatalf(
			"expected DEBIT, got %s",
			entry.Direction(),
		)
	}

	if entry.Value().Amount() != "80.00" {
		t.Fatalf(
			"expected 80.00, got %s",
			entry.Value().Amount(),
		)
	}

	if entry.BalanceBefore().Amount() != "100.00" {
		t.Fatalf(
			"expected balance before 100.00, got %s",
			entry.BalanceBefore().Amount(),
		)
	}

	if entry.BalanceAfter().Amount() != "20.00" {
		t.Fatalf(
			"expected balance after 20.00, got %s",
			entry.BalanceAfter().Amount(),
		)
	}
}

func TestNewCreditLedgerEntry(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("50.00", "BRL")
	after, _ := NewMoney("150.00", "BRL")

	entry, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionCredit,
		value,
		before,
		after,
		walletTestTime(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Direction() != LedgerDirectionCredit {
		t.Fatalf(
			"expected CREDIT, got %s",
			entry.Direction(),
		)
	}
}

func TestLedgerRejectsIncorrectDebitResult(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("80.00", "BRL")

	// O resultado correto seria 20.00.
	after, _ := NewMoney("30.00", "BRL")

	_, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		after,
		walletTestTime(),
	)

	if !errors.Is(err, ErrInvalidLedgerBalance) {
		t.Fatalf(
			"expected ErrInvalidLedgerBalance, got %v",
			err,
		)
	}
}

func TestLedgerRejectsIncorrectCreditResult(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("50.00", "BRL")

	// O resultado correto seria 150.00.
	after, _ := NewMoney("140.00", "BRL")

	_, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionCredit,
		value,
		before,
		after,
		walletTestTime(),
	)

	if !errors.Is(err, ErrInvalidLedgerBalance) {
		t.Fatalf(
			"expected ErrInvalidLedgerBalance, got %v",
			err,
		)
	}
}

func TestLedgerRejectsCurrencyMismatch(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("10.00", "USD")
	after, _ := NewMoney("90.00", "BRL")

	_, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		after,
		walletTestTime(),
	)

	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf(
			"expected ErrCurrencyMismatch, got %v",
			err,
		)
	}
}

func TestLedgerRejectsZeroValue(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := ZeroMoney("BRL")
	after, _ := NewMoney("100.00", "BRL")

	_, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		after,
		walletTestTime(),
	)

	if !errors.Is(err, ErrInvalidLedgerAmount) {
		t.Fatalf(
			"expected ErrInvalidLedgerAmount, got %v",
			err,
		)
	}
}

func TestLedgerRejectsDebitThatWouldGoNegative(t *testing.T) {
	before, _ := NewMoney("20.00", "BRL")
	value, _ := NewMoney("80.00", "BRL")

	negativeAfter, err := MoneyFromMinorUnits(
		-6000,
		"BRL",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		negativeAfter,
		walletTestTime(),
	)

	if !errors.Is(err, ErrInvalidLedgerBalance) {
		t.Fatalf(
			"expected ErrInvalidLedgerBalance, got %v",
			err,
		)
	}
}

func TestLedgerRejectsInvalidDirection(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("10.00", "BRL")
	after, _ := NewMoney("90.00", "BRL")

	_, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirection("INVALID"),
		value,
		before,
		after,
		walletTestTime(),
	)

	if !errors.Is(err, ErrInvalidLedgerDirection) {
		t.Fatalf(
			"expected ErrInvalidLedgerDirection, got %v",
			err,
		)
	}
}

func TestLedgerCreatedAtIsUTC(t *testing.T) {
	before, _ := NewMoney("100.00", "BRL")
	value, _ := NewMoney("10.00", "BRL")
	after, _ := NewMoney("90.00", "BRL")

	location := time.FixedZone("BRT", -3*60*60)

	createdAt := time.Date(
		2026, time.October, 2,
		10, 0, 0, 0,
		location,
	)

	entry, err := NewLedgerEntry(
		"ledger-1",
		"wallet-1",
		"transaction-1",
		LedgerDirectionDebit,
		value,
		before,
		after,
		createdAt,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.CreatedAt().Location() != time.UTC {
		t.Fatal("expected createdAt in UTC")
	}
}
