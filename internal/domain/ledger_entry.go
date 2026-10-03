package domain

import (
	"errors"
	"strings"
	"time"
)

type LedgerDirection string

const (
	LedgerDirectionDebit  LedgerDirection = "DEBIT"
	LedgerDirectionCredit LedgerDirection = "CREDIT"
)

var (
	ErrInvalidLedgerEntryID   = errors.New("invalid ledger entry id")
	ErrInvalidLedgerDirection = errors.New("invalid ledger direction")
	ErrInvalidLedgerBalance   = errors.New("invalid ledger balance")
	ErrInvalidLedgerAmount    = errors.New("invalid ledger amount")
)

type LedgerEntry struct {
	id            string
	walletID      string
	transactionID string
	direction     LedgerDirection
	value         Money
	balanceBefore Money
	balanceAfter  Money
	createdAt     time.Time
}

func NewLedgerEntry(
	id string,
	walletID string,
	transactionID string,
	direction LedgerDirection,
	value Money,
	balanceBefore Money,
	balanceAfter Money,
	createdAt time.Time,
) (*LedgerEntry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidLedgerEntryID
	}

	if strings.TrimSpace(walletID) == "" {
		return nil, ErrInvalidWalletID
	}

	if strings.TrimSpace(transactionID) == "" {
		return nil, ErrInvalidTransactionID
	}

	if direction != LedgerDirectionDebit &&
		direction != LedgerDirectionCredit {
		return nil, ErrInvalidLedgerDirection
	}

	if !value.IsPositive() {
		return nil, ErrInvalidLedgerAmount
	}

	if createdAt.IsZero() {
		return nil, errors.New("ledger creation time cannot be zero")
	}

	if value.Currency() == "" ||
		balanceBefore.Currency() == "" ||
		balanceAfter.Currency() == "" {
		return nil, ErrInvalidCurrency
	}

	if value.Currency() != balanceBefore.Currency() ||
		value.Currency() != balanceAfter.Currency() {
		return nil, ErrCurrencyMismatch
	}

	if balanceBefore.IsNegative() ||
		balanceAfter.IsNegative() {
		return nil, ErrInvalidLedgerBalance
	}

	if err := validateLedgerMovement(
		direction,
		value,
		balanceBefore,
		balanceAfter,
	); err != nil {
		return nil, err
	}

	return &LedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		value:         value,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt.UTC(),
	}, nil
}

func validateLedgerMovement(
	direction LedgerDirection,
	value Money,
	balanceBefore Money,
	balanceAfter Money,
) error {
	var expected Money
	var err error

	switch direction {
	case LedgerDirectionDebit:
		expected, err = balanceBefore.Subtract(value)

	case LedgerDirectionCredit:
		expected, err = balanceBefore.Add(value)

	default:
		return ErrInvalidLedgerDirection
	}

	if err != nil {
		return err
	}

	if expected.IsNegative() {
		return ErrInvalidLedgerBalance
	}

	if !expected.Equal(balanceAfter) {
		return ErrInvalidLedgerBalance
	}

	return nil
}

func (e *LedgerEntry) ID() string {
	return e.id
}

func (e *LedgerEntry) WalletID() string {
	return e.walletID
}

func (e *LedgerEntry) TransactionID() string {
	return e.transactionID
}

func (e *LedgerEntry) Direction() LedgerDirection {
	return e.direction
}

func (e *LedgerEntry) Value() Money {
	return e.value
}

func (e *LedgerEntry) BalanceBefore() Money {
	return e.balanceBefore
}

func (e *LedgerEntry) BalanceAfter() Money {
	return e.balanceAfter
}

func (e *LedgerEntry) CreatedAt() time.Time {
	return e.createdAt
}
