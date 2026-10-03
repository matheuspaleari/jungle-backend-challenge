package domain

import (
	"errors"
	"strings"
	"time"
)

type WagerKind string

const (
	WagerKindOpening  WagerKind = "OPENING"
	WagerKindBet      WagerKind = "BET"
	WagerKindWin      WagerKind = "WIN"
	WagerKindLoss     WagerKind = "LOSS"
	WagerKindRefund   WagerKind = "REFUND"
	WagerKindRollback WagerKind = "ROLLBACK"
)

type WagerStatus string

const (
	WagerStatusPending          WagerStatus = "PENDING"
	WagerStatusPendingReference WagerStatus = "PENDING_REFERENCE"
	WagerStatusProcessed        WagerStatus = "PROCESSED"
	WagerStatusRejected         WagerStatus = "REJECTED"
	WagerStatusFailed           WagerStatus = "FAILED"
)

var (
	ErrInvalidTransactionID         = errors.New("invalid transaction id")
	ErrInvalidExternalTransactionID = errors.New("invalid external transaction id")
	ErrInvalidProviderID            = errors.New("invalid provider id")
	ErrInvalidIdempotencyKey        = errors.New("invalid idempotency key")
	ErrInvalidPayloadHash           = errors.New("invalid payload hash")
	ErrInvalidRoundID               = errors.New("invalid round id")
	ErrInvalidGameID                = errors.New("invalid game id")
	ErrInvalidWagerKind             = errors.New("invalid wager kind")
	ErrInvalidWagerStatus           = errors.New("invalid wager status")
	ErrReferenceRequired            = errors.New("reference external transaction id is required")
	ErrReferenceNotAllowed          = errors.New("reference external transaction id is not allowed")
	ErrOpeningExternalNotAllowed    = errors.New("opening transaction cannot be external")
	ErrInvalidLossAmount            = errors.New("loss amount must be zero")
	ErrInvalidExternalWagerAmount   = errors.New("external wager amount must be positive")
	ErrTransactionTerminal          = errors.New("transaction is already terminal")
	ErrInvalidStateTransition       = errors.New("invalid transaction state transition")
	ErrFailureCodeRequired          = errors.New("failure code is required")
	ErrInvalidResultBalance         = errors.New("invalid transaction result balance")
)

type NewExternalWagerTransactionParams struct {
	ID                    string
	ExternalTransactionID string
	ProviderID            string
	IdempotencyKey        string
	PayloadHash           string
	WalletID              string
	PlayerID              string
	RoundID               string
	GameID                string
	Kind                  WagerKind
	Money                 Money
	ReferenceExternalID   string
	CreatedAt             time.Time
}

type RehydrateWagerTransactionParams struct {
	ID                    string
	ExternalTransactionID string
	ProviderID            string
	IdempotencyKey        string
	PayloadHash           string
	WalletID              string
	PlayerID              string
	RoundID               string
	GameID                string
	Kind                  WagerKind
	Money                 Money
	ReferenceExternalID   string
	ResolvedReferenceID   string
	Status                WagerStatus
	FailureCode           string
	ResultBalance         *Money
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type WagerTransaction struct {
	id                    string
	externalTransactionID string
	providerID            string
	idempotencyKey        string
	payloadHash           string

	walletID string
	playerID string
	roundID  string
	gameID   string

	kind  WagerKind
	money Money

	referenceExternalID string
	resolvedReferenceID string

	status        WagerStatus
	failureCode   string
	resultBalance *Money

	createdAt time.Time
	updatedAt time.Time
}

func NewExternalWagerTransaction(
	params NewExternalWagerTransactionParams,
) (*WagerTransaction, error) {
	if strings.TrimSpace(params.ID) == "" {
		return nil, ErrInvalidTransactionID
	}

	if strings.TrimSpace(params.ExternalTransactionID) == "" {
		return nil, ErrInvalidExternalTransactionID
	}

	if strings.TrimSpace(params.ProviderID) == "" {
		return nil, ErrInvalidProviderID
	}

	if strings.TrimSpace(params.IdempotencyKey) == "" {
		return nil, ErrInvalidIdempotencyKey
	}

	if strings.TrimSpace(params.PayloadHash) == "" {
		return nil, ErrInvalidPayloadHash
	}

	if strings.TrimSpace(params.WalletID) == "" {
		return nil, ErrInvalidWalletID
	}

	if strings.TrimSpace(params.PlayerID) == "" {
		return nil, ErrInvalidPlayerID
	}

	if strings.TrimSpace(params.RoundID) == "" {
		return nil, ErrInvalidRoundID
	}

	if strings.TrimSpace(params.GameID) == "" {
		return nil, ErrInvalidGameID
	}

	if params.CreatedAt.IsZero() {
		return nil, errors.New("transaction creation time cannot be zero")
	}

	if !isExternalWagerKind(params.Kind) {
		if params.Kind == WagerKindOpening {
			return nil, ErrOpeningExternalNotAllowed
		}

		return nil, ErrInvalidWagerKind
	}

	if params.Money.Currency() == "" {
		return nil, ErrInvalidCurrency
	}

	if err := validateExternalWagerMoney(params.Kind, params.Money); err != nil {
		return nil, err
	}

	if requiresReference(params.Kind) {
		if strings.TrimSpace(params.ReferenceExternalID) == "" {
			return nil, ErrReferenceRequired
		}
	} else if strings.TrimSpace(params.ReferenceExternalID) != "" &&
		params.Kind != WagerKindWin {
		return nil, ErrReferenceNotAllowed
	}

	now := params.CreatedAt.UTC()

	return &WagerTransaction{
		id:                    params.ID,
		externalTransactionID: params.ExternalTransactionID,
		providerID:            params.ProviderID,
		idempotencyKey:        params.IdempotencyKey,
		payloadHash:           params.PayloadHash,
		walletID:              params.WalletID,
		playerID:              params.PlayerID,
		roundID:               params.RoundID,
		gameID:                params.GameID,
		kind:                  params.Kind,
		money:                 params.Money,
		referenceExternalID:   params.ReferenceExternalID,
		status:                WagerStatusPending,
		createdAt:             now,
		updatedAt:             now,
	}, nil
}

func RehydrateWagerTransaction(
	params RehydrateWagerTransactionParams,
) (*WagerTransaction, error) {
	if strings.TrimSpace(params.ID) == "" {
		return nil, ErrInvalidTransactionID
	}

	if strings.TrimSpace(params.WalletID) == "" {
		return nil, ErrInvalidWalletID
	}

	if strings.TrimSpace(params.PlayerID) == "" {
		return nil, ErrInvalidPlayerID
	}

	if params.Money.Currency() == "" {
		return nil, ErrInvalidCurrency
	}

	if params.CreatedAt.IsZero() || params.UpdatedAt.IsZero() {
		return nil, errors.New("transaction timestamps cannot be zero")
	}

	if params.UpdatedAt.Before(params.CreatedAt) {
		return nil, errors.New("transaction updated time cannot be before creation time")
	}

	if !isValidWagerStatus(params.Status) {
		return nil, ErrInvalidWagerStatus
	}

	if params.Kind == WagerKindOpening {
		if params.ExternalTransactionID != "" ||
			params.ProviderID != "" ||
			params.IdempotencyKey != "" ||
			params.PayloadHash != "" ||
			params.RoundID != "" ||
			params.GameID != "" ||
			params.ReferenceExternalID != "" {
			return nil, ErrOpeningExternalNotAllowed
		}
	} else {
		if !isExternalWagerKind(params.Kind) {
			return nil, ErrInvalidWagerKind
		}

		if strings.TrimSpace(params.ExternalTransactionID) == "" {
			return nil, ErrInvalidExternalTransactionID
		}

		if strings.TrimSpace(params.ProviderID) == "" {
			return nil, ErrInvalidProviderID
		}

		if strings.TrimSpace(params.IdempotencyKey) == "" {
			return nil, ErrInvalidIdempotencyKey
		}

		if strings.TrimSpace(params.PayloadHash) == "" {
			return nil, ErrInvalidPayloadHash
		}

		if strings.TrimSpace(params.RoundID) == "" {
			return nil, ErrInvalidRoundID
		}

		if strings.TrimSpace(params.GameID) == "" {
			return nil, ErrInvalidGameID
		}

		if err := validateExternalWagerMoney(params.Kind, params.Money); err != nil {
			return nil, err
		}

		if requiresReference(params.Kind) &&
			strings.TrimSpace(params.ReferenceExternalID) == "" {
			return nil, ErrReferenceRequired
		}

		if !requiresReference(params.Kind) &&
			params.Kind != WagerKindWin &&
			strings.TrimSpace(params.ReferenceExternalID) != "" {
			return nil, ErrReferenceNotAllowed
		}
	}

	if params.Status == WagerStatusPendingReference &&
		strings.TrimSpace(params.ReferenceExternalID) == "" {
		return nil, ErrReferenceRequired
	}

	if params.ResolvedReferenceID != "" &&
		params.Kind != WagerKindWin &&
		params.Kind != WagerKindRefund &&
		params.Kind != WagerKindRollback {
		return nil, ErrReferenceNotAllowed
	}

	if (params.Status == WagerStatusRejected ||
		params.Status == WagerStatusFailed) &&
		strings.TrimSpace(params.FailureCode) == "" {
		return nil, ErrFailureCodeRequired
	}

	if params.Status == WagerStatusProcessed &&
		strings.TrimSpace(params.FailureCode) != "" {
		return nil, ErrInvalidStateTransition
	}

	if params.ResultBalance != nil {
		if params.ResultBalance.Currency() != params.Money.Currency() {
			return nil, ErrCurrencyMismatch
		}

		if params.ResultBalance.IsNegative() {
			return nil, ErrInvalidResultBalance
		}
	}

	return &WagerTransaction{
		id:                    params.ID,
		externalTransactionID: params.ExternalTransactionID,
		providerID:            params.ProviderID,
		idempotencyKey:        params.IdempotencyKey,
		payloadHash:           params.PayloadHash,
		walletID:              params.WalletID,
		playerID:              params.PlayerID,
		roundID:               params.RoundID,
		gameID:                params.GameID,
		kind:                  params.Kind,
		money:                 params.Money,
		referenceExternalID:   params.ReferenceExternalID,
		resolvedReferenceID:   params.ResolvedReferenceID,
		status:                params.Status,
		failureCode:           params.FailureCode,
		resultBalance:         copyMoneyPointer(params.ResultBalance),
		createdAt:             params.CreatedAt.UTC(),
		updatedAt:             params.UpdatedAt.UTC(),
	}, nil
}

func isExternalWagerKind(kind WagerKind) bool {
	switch kind {
	case WagerKindBet,
		WagerKindWin,
		WagerKindLoss,
		WagerKindRefund,
		WagerKindRollback:
		return true
	default:
		return false
	}
}

func isValidWagerStatus(status WagerStatus) bool {
	switch status {
	case WagerStatusPending,
		WagerStatusPendingReference,
		WagerStatusProcessed,
		WagerStatusRejected,
		WagerStatusFailed:
		return true
	default:
		return false
	}
}

func requiresReference(kind WagerKind) bool {
	return kind == WagerKindRefund ||
		kind == WagerKindRollback
}

func validateExternalWagerMoney(
	kind WagerKind,
	money Money,
) error {
	switch kind {
	case WagerKindLoss:
		if !money.IsZero() {
			return ErrInvalidLossAmount
		}

	case WagerKindBet,
		WagerKindWin,
		WagerKindRefund,
		WagerKindRollback:
		if !money.IsPositive() {
			return ErrInvalidExternalWagerAmount
		}

	default:
		return ErrInvalidWagerKind
	}

	return nil
}

func (t *WagerTransaction) MarkPendingReference(
	now time.Time,
) error {
	if err := t.ensureTransitionTime(now); err != nil {
		return err
	}

	if t.isTerminal() {
		return ErrTransactionTerminal
	}

	if t.status != WagerStatusPending {
		return ErrInvalidStateTransition
	}

	if !requiresReference(t.kind) &&
		!(t.kind == WagerKindWin && t.referenceExternalID != "") {
		return ErrInvalidStateTransition
	}

	t.status = WagerStatusPendingReference
	t.updatedAt = now.UTC()

	return nil
}

func (t *WagerTransaction) ResolveReference(
	internalTransactionID string,
	now time.Time,
) error {
	if err := t.ensureTransitionTime(now); err != nil {
		return err
	}

	if t.isTerminal() {
		return ErrTransactionTerminal
	}

	if t.status != WagerStatusPendingReference {
		return ErrInvalidStateTransition
	}

	if strings.TrimSpace(internalTransactionID) == "" {
		return ErrInvalidTransactionID
	}

	t.resolvedReferenceID = internalTransactionID
	t.updatedAt = now.UTC()

	return nil
}

func (t *WagerTransaction) MarkProcessed(
	now time.Time,
) error {
	if err := t.ensureTransitionTime(now); err != nil {
		return err
	}

	if t.isTerminal() {
		return ErrTransactionTerminal
	}

	switch t.status {
	case WagerStatusPending:
	case WagerStatusPendingReference:
		if t.resolvedReferenceID == "" {
			return ErrInvalidStateTransition
		}
	default:
		return ErrInvalidStateTransition
	}

	t.status = WagerStatusProcessed
	t.failureCode = ""
	t.updatedAt = now.UTC()

	return nil
}

func (t *WagerTransaction) MarkRejected(
	failureCode string,
	now time.Time,
) error {
	if err := t.ensureTransitionTime(now); err != nil {
		return err
	}

	if t.isTerminal() {
		return ErrTransactionTerminal
	}

	if strings.TrimSpace(failureCode) == "" {
		return ErrFailureCodeRequired
	}

	if t.status != WagerStatusPending &&
		t.status != WagerStatusPendingReference {
		return ErrInvalidStateTransition
	}

	t.status = WagerStatusRejected
	t.failureCode = failureCode
	t.updatedAt = now.UTC()

	return nil
}

func (t *WagerTransaction) MarkFailed(
	failureCode string,
	now time.Time,
) error {
	if err := t.ensureTransitionTime(now); err != nil {
		return err
	}

	if t.isTerminal() {
		return ErrTransactionTerminal
	}

	if strings.TrimSpace(failureCode) == "" {
		return ErrFailureCodeRequired
	}

	if t.status != WagerStatusPending &&
		t.status != WagerStatusPendingReference {
		return ErrInvalidStateTransition
	}

	t.status = WagerStatusFailed
	t.failureCode = failureCode
	t.updatedAt = now.UTC()

	return nil
}

func (t *WagerTransaction) SetResultBalance(balance Money) error {
	if balance.Currency() == "" {
		return ErrInvalidCurrency
	}

	if balance.Currency() != t.money.Currency() {
		return ErrCurrencyMismatch
	}

	if balance.IsNegative() {
		return ErrInvalidResultBalance
	}

	copied := balance
	t.resultBalance = &copied

	return nil
}

func (t *WagerTransaction) isTerminal() bool {
	switch t.status {
	case WagerStatusProcessed,
		WagerStatusRejected,
		WagerStatusFailed:
		return true
	default:
		return false
	}
}

func (t *WagerTransaction) ensureTransitionTime(
	now time.Time,
) error {
	if now.IsZero() {
		return errors.New("transaction transition time cannot be zero")
	}

	if now.Before(t.updatedAt) {
		return errors.New("transaction transition time cannot be before last update")
	}

	return nil
}

func copyMoneyPointer(money *Money) *Money {
	if money == nil {
		return nil
	}

	copied := *money
	return &copied
}

func (t *WagerTransaction) ID() string {
	return t.id
}

func (t *WagerTransaction) ExternalTransactionID() string {
	return t.externalTransactionID
}

func (t *WagerTransaction) ProviderID() string {
	return t.providerID
}

func (t *WagerTransaction) IdempotencyKey() string {
	return t.idempotencyKey
}

func (t *WagerTransaction) PayloadHash() string {
	return t.payloadHash
}

func (t *WagerTransaction) WalletID() string {
	return t.walletID
}

func (t *WagerTransaction) PlayerID() string {
	return t.playerID
}

func (t *WagerTransaction) RoundID() string {
	return t.roundID
}

func (t *WagerTransaction) GameID() string {
	return t.gameID
}

func (t *WagerTransaction) Kind() WagerKind {
	return t.kind
}

func (t *WagerTransaction) Money() Money {
	return t.money
}

func (t *WagerTransaction) ReferenceExternalID() string {
	return t.referenceExternalID
}

func (t *WagerTransaction) ResolvedReferenceID() string {
	return t.resolvedReferenceID
}

func (t *WagerTransaction) Status() WagerStatus {
	return t.status
}

func (t *WagerTransaction) FailureCode() string {
	return t.failureCode
}

func (t *WagerTransaction) ResultBalance() (Money, bool) {
	if t.resultBalance == nil {
		return Money{}, false
	}

	return *t.resultBalance, true
}

func (t *WagerTransaction) CreatedAt() time.Time {
	return t.createdAt
}

func (t *WagerTransaction) UpdatedAt() time.Time {
	return t.updatedAt
}
