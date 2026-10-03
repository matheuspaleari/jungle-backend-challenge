package domain

import (
	"errors"
	"testing"
	"time"
)

func walletTestTime() time.Time {
	return time.Date(
		2026, time.October, 2,
		12, 0, 0, 0,
		time.UTC,
	)
}

func TestNewWallet(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	now := walletTestTime()

	wallet, err := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wallet.ID() != "wallet-1" {
		t.Fatalf("unexpected wallet id: %s", wallet.ID())
	}

	if wallet.PlayerID() != "player-1" {
		t.Fatalf("unexpected player id: %s", wallet.PlayerID())
	}

	if wallet.Currency() != "BRL" {
		t.Fatalf("unexpected currency: %s", wallet.Currency())
	}

	if wallet.Balance().Amount() != "100.00" {
		t.Fatalf(
			"expected 100.00, got %s",
			wallet.Balance().Amount(),
		)
	}

	if wallet.Version() != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			wallet.Version(),
		)
	}
}

func TestNewWalletAllowsZeroBalance(t *testing.T) {
	initialBalance, _ := NewMoney("0.00", "BRL")

	wallet, err := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		walletTestTime(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wallet.Balance().IsZero() {
		t.Fatal("expected zero balance")
	}
}

func TestWalletDebit(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	debit, _ := NewMoney("25.00", "BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Debit(
		debit,
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wallet.Balance().Amount() != "75.00" {
		t.Fatalf(
			"expected 75.00, got %s",
			wallet.Balance().Amount(),
		)
	}

	if wallet.Version() != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			wallet.Version(),
		)
	}
}

func TestWalletDebitCanReachZero(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	debit, _ := NewMoney("100.00", "BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Debit(
		debit,
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wallet.Balance().IsZero() {
		t.Fatalf(
			"expected zero balance, got %s",
			wallet.Balance().Amount(),
		)
	}
}

func TestWalletRejectsInsufficientFunds(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	debit, _ := NewMoney("100.01", "BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Debit(
		debit,
		now.Add(time.Second),
	)

	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf(
			"expected ErrInsufficientFunds, got %v",
			err,
		)
	}

	if wallet.Balance().Amount() != "100.00" {
		t.Fatalf(
			"balance changed after rejected debit: %s",
			wallet.Balance().Amount(),
		)
	}

	if wallet.Version() != 1 {
		t.Fatalf(
			"version changed after rejected debit: %d",
			wallet.Version(),
		)
	}
}

func TestWalletCredit(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	credit, _ := NewMoney("50.00", "BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Credit(
		credit,
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wallet.Balance().Amount() != "150.00" {
		t.Fatalf(
			"expected 150.00, got %s",
			wallet.Balance().Amount(),
		)
	}

	if wallet.Version() != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			wallet.Version(),
		)
	}
}

func TestWalletRejectsCurrencyMismatch(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	usd, _ := NewMoney("10.00", "USD")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Debit(
		usd,
		now.Add(time.Second),
	)

	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf(
			"expected ErrCurrencyMismatch, got %v",
			err,
		)
	}

	if wallet.Balance().Amount() != "100.00" {
		t.Fatal("balance changed after currency mismatch")
	}

	if wallet.Version() != 1 {
		t.Fatal("version changed after currency mismatch")
	}
}

func TestWalletRejectsZeroDebit(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	zero, _ := ZeroMoney("BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Debit(
		zero,
		now.Add(time.Second),
	)

	if !errors.Is(err, ErrInvalidDebitAmount) {
		t.Fatalf(
			"expected ErrInvalidDebitAmount, got %v",
			err,
		)
	}
}

func TestWalletRejectsZeroCredit(t *testing.T) {
	initialBalance, _ := NewMoney("100.00", "BRL")
	zero, _ := ZeroMoney("BRL")

	now := walletTestTime()

	wallet, _ := NewWallet(
		"wallet-1",
		"player-1",
		initialBalance,
		now,
	)

	err := wallet.Credit(
		zero,
		now.Add(time.Second),
	)

	if !errors.Is(err, ErrInvalidCreditAmount) {
		t.Fatalf(
			"expected ErrInvalidCreditAmount, got %v",
			err,
		)
	}
}

func TestRehydrateWalletPreservesState(t *testing.T) {
	balance, _ := NewMoney("42.50", "BRL")

	createdAt := walletTestTime()
	updatedAt := createdAt.Add(time.Hour)

	wallet, err := RehydrateWallet(
		"wallet-1",
		"player-1",
		"BRL",
		balance,
		7,
		createdAt,
		updatedAt,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wallet.Balance().Amount() != "42.50" {
		t.Fatalf(
			"expected 42.50, got %s",
			wallet.Balance().Amount(),
		)
	}

	if wallet.Version() != 7 {
		t.Fatalf(
			"expected version 7, got %d",
			wallet.Version(),
		)
	}

	if !wallet.UpdatedAt().Equal(updatedAt) {
		t.Fatal("updatedAt was not preserved")
	}
}
