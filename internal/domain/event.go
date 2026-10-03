package domain

import (
	"errors"
	"strings"
	"time"
)

type EventType string

const (
	EventTypeWagerTransactionProcessed EventType = "WagerTransactionProcessed"
	EventTypeWagerTransactionRejected  EventType = "WagerTransactionRejected"
	EventTypeWalletBalanceChanged      EventType = "WalletBalanceChanged"
	EventTypePendingReference          EventType = "WagerTransactionPendingReference"
)

var (
	ErrInvalidEventID        = errors.New("invalid event id")
	ErrInvalidEventType      = errors.New("invalid event type")
	ErrInvalidAggregateID    = errors.New("invalid aggregate id")
	ErrInvalidCorrelationID  = errors.New("invalid correlation id")
	ErrInvalidEventVersion   = errors.New("invalid event version")
	ErrInvalidEventTimestamp = errors.New("invalid event timestamp")
	ErrInvalidEventData      = errors.New("invalid event data")
)

type Event struct {
	eventID       string
	eventType     EventType
	aggregateID   string
	correlationID string
	causationID   string
	occurredAt    time.Time
	version       uint64
	data          any
}

func NewEvent(
	eventID string,
	eventType EventType,
	aggregateID string,
	correlationID string,
	causationID string,
	occurredAt time.Time,
	version uint64,
	data any,
) (*Event, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, ErrInvalidEventID
	}

	if !isValidEventType(eventType) {
		return nil, ErrInvalidEventType
	}

	if strings.TrimSpace(aggregateID) == "" {
		return nil, ErrInvalidAggregateID
	}

	if strings.TrimSpace(correlationID) == "" {
		return nil, ErrInvalidCorrelationID
	}

	if occurredAt.IsZero() {
		return nil, ErrInvalidEventTimestamp
	}

	if version < 1 {
		return nil, ErrInvalidEventVersion
	}

	if data == nil {
		return nil, ErrInvalidEventData
	}

	return &Event{
		eventID:       eventID,
		eventType:     eventType,
		aggregateID:   aggregateID,
		correlationID: correlationID,
		causationID:   causationID,
		occurredAt:    occurredAt.UTC(),
		version:       version,
		data:          data,
	}, nil
}

func isValidEventType(eventType EventType) bool {
	switch eventType {
	case EventTypeWagerTransactionProcessed,
		EventTypeWagerTransactionRejected,
		EventTypeWalletBalanceChanged,
		EventTypePendingReference:
		return true
	default:
		return false
	}
}

func (e *Event) EventID() string {
	return e.eventID
}

func (e *Event) EventType() EventType {
	return e.eventType
}

func (e *Event) AggregateID() string {
	return e.aggregateID
}

func (e *Event) CorrelationID() string {
	return e.correlationID
}

func (e *Event) CausationID() string {
	return e.causationID
}

func (e *Event) OccurredAt() time.Time {
	return e.occurredAt
}

func (e *Event) Version() uint64 {
	return e.version
}

func (e *Event) Data() any {
	return e.data
}

type MoneySnapshot struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func NewMoneySnapshot(money Money) MoneySnapshot {
	return MoneySnapshot{
		Amount:   money.Amount(),
		Currency: money.Currency(),
	}
}

type WagerTransactionProcessedData struct {
	TransactionID         string        `json:"transactionId"`
	ExternalTransactionID string        `json:"externalTransactionId"`
	ProviderID            string        `json:"providerId"`
	WalletID              string        `json:"walletId"`
	PlayerID              string        `json:"playerId"`
	Kind                  WagerKind     `json:"kind"`
	Money                 MoneySnapshot `json:"money"`
}

type WagerTransactionRejectedData struct {
	TransactionID         string    `json:"transactionId"`
	ExternalTransactionID string    `json:"externalTransactionId"`
	ProviderID            string    `json:"providerId"`
	WalletID              string    `json:"walletId"`
	PlayerID              string    `json:"playerId"`
	Kind                  WagerKind `json:"kind"`
	FailureCode           string    `json:"failureCode"`
}

type PendingReferenceData struct {
	TransactionID         string    `json:"transactionId"`
	ExternalTransactionID string    `json:"externalTransactionId"`
	ProviderID            string    `json:"providerId"`
	WalletID              string    `json:"walletId"`
	Kind                  WagerKind `json:"kind"`
	ReferenceExternalID   string    `json:"referenceExternalTransactionId"`
}

type WalletBalanceChangedData struct {
	WalletID      string          `json:"walletId"`
	TransactionID string          `json:"transactionId"`
	Direction     LedgerDirection `json:"direction"`
	Money         MoneySnapshot   `json:"money"`
	BalanceBefore MoneySnapshot   `json:"balanceBefore"`
	BalanceAfter  MoneySnapshot   `json:"balanceAfter"`
	WalletVersion uint64          `json:"walletVersion"`
}
