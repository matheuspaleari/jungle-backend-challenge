package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidWalletID      = errors.New("invalid wallet id")
	ErrInvalidPlayerID      = errors.New("invalid player id")
	ErrInvalidWalletVersion = errors.New("invalid wallet version")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrInvalidDebitAmount   = errors.New("invalid debit amount")
	ErrInvalidCreditAmount  = errors.New("invalid credit amount")
)

type Wallet struct {
	id         string
	playerID   string
	providerID string
	currency   string
	balance    Money
	version    uint64
	createdAt  time.Time
	updatedAt  time.Time
}

func NewWallet(
	id string,
	playerID string,
	initialBalance Money,
	now time.Time,
) (*Wallet, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidWalletID
	}

	if strings.TrimSpace(playerID) == "" {
		return nil, ErrInvalidPlayerID
	}

	if initialBalance.Currency() == "" {
		return nil, ErrInvalidCurrency
	}

	if initialBalance.IsNegative() {
		return nil, ErrInvalidMoneyAmount
	}

	if now.IsZero() {
		return nil, errors.New("wallet creation time cannot be zero")
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  initialBalance.Currency(),
		balance:   initialBalance,
		version:   1,
		createdAt: now.UTC(),
		updatedAt: now.UTC(),
	}, nil
}

func NewWalletForProvider(
	id string,
	providerID string,
	playerID string,
	initialBalance Money,
	now time.Time,
) (*Wallet, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, ErrInvalidProviderID
	}

	wallet, err := NewWallet(id, playerID, initialBalance, now)
	if err != nil {
		return nil, err
	}
	wallet.providerID = providerID
	return wallet, nil
}

func RehydrateWallet(
	id string,
	playerID string,
	currency string,
	balance Money,
	version uint64,
	createdAt time.Time,
	updatedAt time.Time,
) (*Wallet, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidWalletID
	}

	if strings.TrimSpace(playerID) == "" {
		return nil, ErrInvalidPlayerID
	}

	if currency == "" || balance.Currency() != currency {
		return nil, ErrCurrencyMismatch
	}

	if balance.IsNegative() {
		return nil, ErrInvalidMoneyAmount
	}

	if version < 1 {
		return nil, ErrInvalidWalletVersion
	}

	if createdAt.IsZero() || updatedAt.IsZero() {
		return nil, errors.New("wallet timestamps cannot be zero")
	}

	if updatedAt.Before(createdAt) {
		return nil, errors.New("wallet updated time cannot be before creation time")
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  currency,
		balance:   balance,
		version:   version,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
	}, nil
}

func RehydrateWalletForProvider(
	id string,
	providerID string,
	playerID string,
	currency string,
	balance Money,
	version uint64,
	createdAt time.Time,
	updatedAt time.Time,
) (*Wallet, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return RehydrateWallet(id, playerID, currency, balance, version, createdAt, updatedAt)
	}

	wallet, err := RehydrateWallet(id, playerID, currency, balance, version, createdAt, updatedAt)
	if err != nil {
		return nil, err
	}
	wallet.providerID = providerID
	return wallet, nil
}

func (w *Wallet) Debit(amount Money, now time.Time) error {
	if err := w.validateOperation(amount, now); err != nil {
		return err
	}

	if !amount.IsPositive() {
		return ErrInvalidDebitAmount
	}

	comparison, err := w.balance.Compare(amount)
	if err != nil {
		return err
	}

	if comparison < 0 {
		return ErrInsufficientFunds
	}

	newBalance, err := w.balance.Subtract(amount)
	if err != nil {
		return err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = now.UTC()

	return nil
}

func (w *Wallet) Credit(amount Money, now time.Time) error {
	if err := w.validateOperation(amount, now); err != nil {
		return err
	}

	if !amount.IsPositive() {
		return ErrInvalidCreditAmount
	}

	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = now.UTC()

	return nil
}

func (w *Wallet) validateOperation(amount Money, now time.Time) error {
	if amount.Currency() == "" {
		return ErrInvalidCurrency
	}

	if amount.Currency() != w.currency {
		return ErrCurrencyMismatch
	}

	if now.IsZero() {
		return errors.New("wallet operation time cannot be zero")
	}

	if now.Before(w.updatedAt) {
		return errors.New("wallet operation time cannot be before last update")
	}

	return nil
}

func (w *Wallet) ID() string {
	return w.id
}

func (w *Wallet) PlayerID() string {
	return w.playerID
}

func (w *Wallet) ProviderID() string {
	return w.providerID
}

func (w *Wallet) Currency() string {
	return w.currency
}

func (w *Wallet) Balance() Money {
	return w.balance
}

func (w *Wallet) Version() uint64 {
	return w.version
}

func (w *Wallet) CreatedAt() time.Time {
	return w.createdAt
}

func (w *Wallet) UpdatedAt() time.Time {
	return w.updatedAt
}
