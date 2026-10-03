package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

var (
	ErrWagerRepositoryPoolRequired = errors.New(
		"wager repository postgres pool is required",
	)

	ErrWagerRepositoryTxRequired = errors.New(
		"wager repository transaction is required",
	)
)

type WagerRepository struct {
	pool *pgxpool.Pool
}

func NewWagerRepository(
	pool *pgxpool.Pool,
) (*WagerRepository, error) {
	if pool == nil {
		return nil, ErrWagerRepositoryPoolRequired
	}

	return &WagerRepository{
		pool: pool,
	}, nil
}

func (r *WagerRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}

	if tx == nil {
		return ErrWagerRepositoryTxRequired
	}

	if transaction == nil {
		return errors.New("wager transaction is required")
	}

	resultBalanceMinor := nullableResultBalance(transaction)

	_, err := tx.Exec(
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
				reference_external_transaction_id,
				resolved_reference_transaction_id,
				state,
				failure_code,
				result_balance_minor,
				created_at,
				updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15,
				$16, $17, $18, $19
			)
		`,
		transaction.ID(),
		nullableString(transaction.ExternalTransactionID()),
		nullableString(transaction.ProviderID()),
		nullableString(transaction.IdempotencyKey()),
		nullableString(transaction.PayloadHash()),
		transaction.WalletID(),
		transaction.PlayerID(),
		nullableString(transaction.RoundID()),
		nullableString(transaction.GameID()),
		string(transaction.Kind()),
		transaction.Money().MinorUnits(),
		transaction.Money().Currency(),
		nullableString(transaction.ReferenceExternalID()),
		nullableString(transaction.ResolvedReferenceID()),
		string(transaction.Status()),
		nullableString(transaction.FailureCode()),
		resultBalanceMinor,
		transaction.CreatedAt(),
		transaction.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf(
			"insert wager transaction: %w",
			err,
		)
	}

	return nil
}

func (r *WagerRepository) Update(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}

	if tx == nil {
		return ErrWagerRepositoryTxRequired
	}

	if transaction == nil {
		return errors.New("wager transaction is required")
	}

	result, err := tx.Exec(
		ctx,
		`
			UPDATE wager_transactions
			SET
				resolved_reference_transaction_id = $1,
				state = $2,
				failure_code = $3,
				result_balance_minor = $4,
				updated_at = $5,
				reference_next_attempt_at = NULL,
				reference_expires_at = NULL
			WHERE id = $6
		`,
		nullableString(transaction.ResolvedReferenceID()),
		string(transaction.Status()),
		nullableString(transaction.FailureCode()),
		nullableResultBalance(transaction),
		transaction.UpdatedAt(),
		transaction.ID(),
	)
	if err != nil {
		return fmt.Errorf(
			"update wager transaction: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}

	return nil
}

type PendingReferenceSchedule struct {
	AttemptCount  int
	NextAttemptAt time.Time
	ExpiresAt     time.Time
}

func (r *WagerRepository) UpdatePendingReference(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	schedule PendingReferenceSchedule,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if tx == nil {
		return ErrWagerRepositoryTxRequired
	}
	if transaction == nil {
		return errors.New("wager transaction is required")
	}
	if transaction.Status() != domain.WagerStatusPendingReference {
		return errors.New("wager transaction must be PENDING_REFERENCE")
	}
	if schedule.AttemptCount < 0 {
		return errors.New("reference attempt count cannot be negative")
	}
	if schedule.NextAttemptAt.IsZero() || schedule.ExpiresAt.IsZero() {
		return errors.New("reference retry schedule is required")
	}
	if schedule.ExpiresAt.Before(schedule.NextAttemptAt) {
		return errors.New("reference expiration cannot be before next attempt")
	}

	result, err := tx.Exec(
		ctx,
		`
			UPDATE wager_transactions
			SET
				resolved_reference_transaction_id = $1,
				state = $2,
				failure_code = $3,
				result_balance_minor = $4,
				updated_at = $5,
				reference_attempt_count = $6,
				reference_next_attempt_at = $7,
				reference_expires_at = $8
			WHERE id = $9
		`,
		nullableString(transaction.ResolvedReferenceID()),
		string(transaction.Status()),
		nullableString(transaction.FailureCode()),
		nullableResultBalance(transaction),
		transaction.UpdatedAt(),
		schedule.AttemptCount,
		schedule.NextAttemptAt.UTC(),
		schedule.ExpiresAt.UTC(),
		transaction.ID(),
	)
	if err != nil {
		return fmt.Errorf("update pending reference: %w", err)
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *WagerRepository) HasProcessedReversal(
	ctx context.Context,
	tx pgx.Tx,
	resolvedReferenceID string,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("context is required")
	}
	if tx == nil {
		return false, ErrWagerRepositoryTxRequired
	}
	resolvedReferenceID = strings.TrimSpace(resolvedReferenceID)
	if resolvedReferenceID == "" {
		return false, domain.ErrInvalidTransactionID
	}

	var exists bool
	err := tx.QueryRow(
		ctx,
		`
			SELECT EXISTS (
				SELECT 1
				FROM wager_transactions
				WHERE resolved_reference_transaction_id = $1
				  AND kind IN ('REFUND', 'ROLLBACK')
				  AND state = 'PROCESSED'
			)
		`,
		resolvedReferenceID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check processed reversal: %w", err)
	}
	return exists, nil
}

func (r *WagerRepository) GetByID(
	ctx context.Context,
	transactionID string,
) (*domain.WagerTransaction, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return nil, domain.ErrInvalidTransactionID
	}

	row := r.pool.QueryRow(
		ctx,
		wagerSelectSQL+`
			WHERE id = $1
		`,
		transactionID,
	)

	return scanWagerTransaction(row)
}

func (r *WagerRepository) GetByProviderAndExternalID(
	ctx context.Context,
	providerID string,
	externalTransactionID string,
) (*domain.WagerTransaction, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	providerID = strings.TrimSpace(providerID)
	externalTransactionID = strings.TrimSpace(externalTransactionID)

	if providerID == "" {
		return nil, domain.ErrInvalidProviderID
	}

	if externalTransactionID == "" {
		return nil, domain.ErrInvalidExternalTransactionID
	}

	row := r.pool.QueryRow(
		ctx,
		wagerSelectSQL+`
			WHERE provider_id = $1
			  AND external_transaction_id = $2
		`,
		providerID,
		externalTransactionID,
	)

	return scanWagerTransaction(row)
}

func (r *WagerRepository) GetByIdempotencyKey(
	ctx context.Context,
	providerID string,
	idempotencyKey string,
) (*domain.WagerTransaction, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	providerID = strings.TrimSpace(providerID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)

	if providerID == "" {
		return nil, domain.ErrInvalidProviderID
	}

	if idempotencyKey == "" {
		return nil, domain.ErrInvalidIdempotencyKey
	}

	row := r.pool.QueryRow(
		ctx,
		wagerSelectSQL+`
			WHERE provider_id = $1
			  AND idempotency_key = $2
		`,
		providerID,
		idempotencyKey,
	)

	return scanWagerTransaction(row)
}

func (r *WagerRepository) GetByProviderAndExternalIDTx(
	ctx context.Context,
	tx pgx.Tx,
	providerID string,
	externalTransactionID string,
) (*domain.WagerTransaction, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if tx == nil {
		return nil, ErrWagerRepositoryTxRequired
	}
	providerID = strings.TrimSpace(providerID)
	externalTransactionID = strings.TrimSpace(externalTransactionID)
	if providerID == "" {
		return nil, domain.ErrInvalidProviderID
	}
	if externalTransactionID == "" {
		return nil, domain.ErrInvalidExternalTransactionID
	}
	return scanWagerTransaction(tx.QueryRow(ctx, wagerSelectSQL+`
		WHERE provider_id = $1 AND external_transaction_id = $2
	`, providerID, externalTransactionID))
}

func (r *WagerRepository) GetByIdempotencyKeyTx(
	ctx context.Context,
	tx pgx.Tx,
	providerID string,
	idempotencyKey string,
) (*domain.WagerTransaction, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if tx == nil {
		return nil, ErrWagerRepositoryTxRequired
	}
	providerID = strings.TrimSpace(providerID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if providerID == "" {
		return nil, domain.ErrInvalidProviderID
	}
	if idempotencyKey == "" {
		return nil, domain.ErrInvalidIdempotencyKey
	}
	return scanWagerTransaction(tx.QueryRow(ctx, wagerSelectSQL+`
		WHERE provider_id = $1 AND idempotency_key = $2
	`, providerID, idempotencyKey))
}

const wagerSelectSQL = `
	SELECT
		id::text,
		COALESCE(external_transaction_id, ''),
		COALESCE(provider_id, ''),
		COALESCE(idempotency_key, ''),
		COALESCE(payload_hash, ''),
		wallet_id::text,
		player_id,
		COALESCE(round_id, ''),
		COALESCE(game_id, ''),
		kind,
		amount_minor,
		currency,
		COALESCE(reference_external_transaction_id, ''),
		COALESCE(resolved_reference_transaction_id::text, ''),
		state,
		COALESCE(failure_code, ''),
		result_balance_minor,
		created_at,
		updated_at
	FROM wager_transactions
`

type wagerRow interface {
	Scan(dest ...any) error
}

func scanWagerTransaction(
	row wagerRow,
) (*domain.WagerTransaction, error) {
	var (
		id                    string
		externalTransactionID string
		providerID            string
		idempotencyKey        string
		payloadHash           string
		walletID              string
		playerID              string
		roundID               string
		gameID                string
		kind                  string
		amountMinor           int64
		currency              string
		referenceExternalID   string
		resolvedReferenceID   string
		status                string
		failureCode           string
		resultBalanceMinor    *int64
		createdAt             time.Time
		updatedAt             time.Time
	)

	err := row.Scan(
		&id,
		&externalTransactionID,
		&providerID,
		&idempotencyKey,
		&payloadHash,
		&walletID,
		&playerID,
		&roundID,
		&gameID,
		&kind,
		&amountMinor,
		&currency,
		&referenceExternalID,
		&resolvedReferenceID,
		&status,
		&failureCode,
		&resultBalanceMinor,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}

		return nil, fmt.Errorf(
			"scan wager transaction: %w",
			err,
		)
	}

	money, err := domain.MoneyFromMinorUnits(
		amountMinor,
		currency,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate wager money: %w",
			err,
		)
	}

	var resultBalance *domain.Money

	if resultBalanceMinor != nil {
		balance, err := domain.MoneyFromMinorUnits(
			*resultBalanceMinor,
			currency,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"rehydrate wager result balance: %w",
				err,
			)
		}

		resultBalance = &balance
	}

	transaction, err := domain.RehydrateWagerTransaction(
		domain.RehydrateWagerTransactionParams{
			ID:                    id,
			ExternalTransactionID: externalTransactionID,
			ProviderID:            providerID,
			IdempotencyKey:        idempotencyKey,
			PayloadHash:           payloadHash,
			WalletID:              walletID,
			PlayerID:              playerID,
			RoundID:               roundID,
			GameID:                gameID,
			Kind:                  domain.WagerKind(kind),
			Money:                 money,
			ReferenceExternalID:   referenceExternalID,
			ResolvedReferenceID:   resolvedReferenceID,
			Status:                domain.WagerStatus(status),
			FailureCode:           failureCode,
			ResultBalance:         resultBalance,
			CreatedAt:             createdAt,
			UpdatedAt:             updatedAt,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate wager transaction: %w",
			err,
		)
	}

	return transaction, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return value
}

func nullableResultBalance(
	transaction *domain.WagerTransaction,
) any {
	balance, ok := transaction.ResultBalance()
	if !ok {
		return nil
	}

	return balance.MinorUnits()
}
