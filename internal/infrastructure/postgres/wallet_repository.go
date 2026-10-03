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
	ErrWalletRepositoryPoolRequired = errors.New(
		"wallet repository postgres pool is required",
	)

	ErrWalletRepositoryTxRequired = errors.New(
		"wallet repository transaction is required",
	)

	ErrWalletVersionConflict = errors.New(
		"wallet version conflict",
	)
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(
	pool *pgxpool.Pool,
) (*WalletRepository, error) {
	if pool == nil {
		return nil, ErrWalletRepositoryPoolRequired
	}

	return &WalletRepository{
		pool: pool,
	}, nil
}

func (r *WalletRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	wallet *domain.Wallet,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if tx == nil {
		return ErrWalletRepositoryTxRequired
	}
	if wallet == nil {
		return errors.New("wallet is required")
	}

	_, err := tx.Exec(
		ctx,
		`
			INSERT INTO wallets (
				id,
				player_id,
				provider_id,
				currency,
				balance_minor,
				version,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
		wallet.ID(),
		wallet.PlayerID(),
		nullIfBlank(wallet.ProviderID()),
		wallet.Currency(),
		wallet.Balance().MinorUnits(),
		wallet.Version(),
		wallet.CreatedAt(),
		wallet.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) GetByID(
	ctx context.Context,
	walletID string,
) (*domain.Wallet, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	walletID = strings.TrimSpace(walletID)

	if walletID == "" {
		return nil, domain.ErrInvalidWalletID
	}

	row := r.pool.QueryRow(
		ctx,
		`
			SELECT
				id::text,
				player_id,
				provider_id,
				currency,
				balance_minor,
				version,
				created_at,
				updated_at
			FROM wallets
			WHERE id = $1
		`,
		walletID,
	)

	return scanWallet(row)
}

func (r *WalletRepository) GetByIDForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	walletID string,
) (*domain.Wallet, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	if tx == nil {
		return nil, ErrWalletRepositoryTxRequired
	}

	walletID = strings.TrimSpace(walletID)

	if walletID == "" {
		return nil, domain.ErrInvalidWalletID
	}

	row := tx.QueryRow(
		ctx,
		`
			SELECT
				id::text,
				player_id,
				provider_id,
				currency,
				balance_minor,
				version,
				created_at,
				updated_at
			FROM wallets
			WHERE id = $1
			FOR UPDATE
		`,
		walletID,
	)

	return scanWallet(row)
}

func (r *WalletRepository) Update(
	ctx context.Context,
	tx pgx.Tx,
	wallet *domain.Wallet,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}

	if tx == nil {
		return ErrWalletRepositoryTxRequired
	}

	if wallet == nil {
		return errors.New("wallet is required")
	}

	if wallet.Version() <= 1 {
		return fmt.Errorf(
			"%w: wallet version must be greater than 1 when updating",
			ErrWalletVersionConflict,
		)
	}

	previousVersion := wallet.Version() - 1

	result, err := tx.Exec(
		ctx,
		`
			UPDATE wallets
			SET
				balance_minor = $1,
				version = $2,
				updated_at = $3
			WHERE id = $4
			  AND version = $5
		`,
		wallet.Balance().MinorUnits(),
		wallet.Version(),
		wallet.UpdatedAt(),
		wallet.ID(),
		previousVersion,
	)
	if err != nil {
		return fmt.Errorf(
			"update wallet: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return fmt.Errorf(
			"%w: wallet %s expected version %d",
			ErrWalletVersionConflict,
			wallet.ID(),
			previousVersion,
		)
	}

	return nil
}

type walletRow interface {
	Scan(dest ...any) error
}

func scanWallet(
	row walletRow,
) (*domain.Wallet, error) {
	var (
		id           string
		playerID     string
		providerID   *string
		currency     string
		balanceMinor int64
		version      int64
		createdAt    time.Time
		updatedAt    time.Time
	)

	if err := row.Scan(
		&id,
		&playerID,
		&providerID,
		&currency,
		&balanceMinor,
		&version,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}

		return nil, fmt.Errorf(
			"scan wallet: %w",
			err,
		)
	}

	if version < 1 {
		return nil, fmt.Errorf(
			"invalid wallet version from database: %d",
			version,
		)
	}

	balance, err := domain.MoneyFromMinorUnits(
		balanceMinor,
		currency,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate wallet balance: %w",
			err,
		)
	}

	provider := ""
	if providerID != nil {
		provider = *providerID
	}

	wallet, err := domain.RehydrateWalletForProvider(
		id,
		provider,
		playerID,
		currency,
		balance,
		uint64(version),
		createdAt,
		updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"rehydrate wallet: %w",
			err,
		)
	}

	return wallet, nil
}

func nullIfBlank(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
