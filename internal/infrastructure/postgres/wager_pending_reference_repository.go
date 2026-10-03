package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

type PendingReferenceRecord struct {
	Transaction   *domain.WagerTransaction
	AttemptCount  int
	NextAttemptAt time.Time
	ExpiresAt     time.Time
}

func (r *WagerRepository) ClaimDuePendingReference(
	ctx context.Context,
	tx pgx.Tx,
	now time.Time,
) (*PendingReferenceRecord, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	if tx == nil {
		return nil, ErrWagerRepositoryTxRequired
	}

	row := tx.QueryRow(
		ctx,
		`
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
				updated_at,
				reference_attempt_count,
				reference_next_attempt_at,
				reference_expires_at
			FROM wager_transactions
			WHERE state = 'PENDING_REFERENCE'
			  AND reference_next_attempt_at <= $1
			ORDER BY
				reference_next_attempt_at,
				created_at,
				id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`,
		now.UTC(),
	)

	return scanPendingReferenceRecord(row)
}

func (r *WagerRepository) ReschedulePendingReference(
	ctx context.Context,
	tx pgx.Tx,
	transactionID string,
	attemptCount int,
	nextAttemptAt time.Time,
	updatedAt time.Time,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}

	if tx == nil {
		return ErrWagerRepositoryTxRequired
	}

	if attemptCount < 0 {
		return errors.New(
			"reference attempt count cannot be negative",
		)
	}

	result, err := tx.Exec(
		ctx,
		`
			UPDATE wager_transactions
			SET
				reference_attempt_count = $1,
				reference_next_attempt_at = $2,
				updated_at = $3
			WHERE id = $4
			  AND state = 'PENDING_REFERENCE'
		`,
		attemptCount,
		nextAttemptAt.UTC(),
		updatedAt.UTC(),
		transactionID,
	)
	if err != nil {
		return fmt.Errorf(
			"reschedule pending reference: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}

	return nil
}

func scanPendingReferenceRecord(
	row pgx.Row,
) (*PendingReferenceRecord, error) {
	var (
		id                  string
		externalID          string
		providerID          string
		idempotencyKey      string
		payloadHash         string
		walletID            string
		playerID            string
		kind                string
		currency            string
		roundID             string
		gameID              string
		referenceExternalID string
		resolvedReferenceID string
		failureCode         string
		state               string
		amountMinor         int64
		resultBalanceMinor  *int64
		createdAt           time.Time
		updatedAt           time.Time
		attemptCount        int
		nextAttemptAt       time.Time
		expiresAt           time.Time
	)

	err := row.Scan(
		&id,
		&externalID,
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
		&state,
		&failureCode,
		&resultBalanceMinor,
		&createdAt,
		&updatedAt,
		&attemptCount,
		&nextAttemptAt,
		&expiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}

		return nil, fmt.Errorf(
			"scan pending reference: %w",
			err,
		)
	}

	money, err := domain.MoneyFromMinorUnits(
		amountMinor,
		currency,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate pending wager money: %w",
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
				"rehydrate pending result balance: %w",
				err,
			)
		}

		resultBalance = &balance
	}

	transaction, err := domain.RehydrateWagerTransaction(
		domain.RehydrateWagerTransactionParams{
			ID:                    id,
			ExternalTransactionID: externalID,
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
			Status:                domain.WagerStatus(state),
			FailureCode:           failureCode,
			ResultBalance:         resultBalance,
			CreatedAt:             createdAt,
			UpdatedAt:             updatedAt,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate pending wager: %w",
			err,
		)
	}

	return &PendingReferenceRecord{
		Transaction:   transaction,
		AttemptCount:  attemptCount,
		NextAttemptAt: nextAttemptAt.UTC(),
		ExpiresAt:     expiresAt.UTC(),
	}, nil
}
