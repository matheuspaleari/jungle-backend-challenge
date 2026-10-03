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

var ErrLedgerRepositoryTxRequired = errors.New(
	"ledger repository transaction is required",
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pools ...*pgxpool.Pool) *LedgerRepository {
	var pool *pgxpool.Pool
	if len(pools) > 0 {
		pool = pools[0]
	}
	return &LedgerRepository{pool: pool}
}

type LedgerRecord struct {
	ID            string
	WalletID      string
	TransactionID string
	Direction     domain.LedgerDirection
	Value         domain.Money
	BalanceBefore domain.Money
	BalanceAfter  domain.Money
	CreatedAt     time.Time
}

type LedgerPage struct {
	Entries    []LedgerRecord
	NextCursor string
}

type ReconciliationSnapshot struct {
	StoredBalance     domain.Money
	CalculatedBalance domain.Money
	CheckedEntries    int64
}

func (r *LedgerRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}

	if tx == nil {
		return ErrLedgerRepositoryTxRequired
	}

	if entry == nil {
		return errors.New("ledger entry is required")
	}

	_, err := tx.Exec(
		ctx,
		`
			INSERT INTO ledger_entries (
				id,
				wallet_id,
				transaction_id,
				direction,
				value_minor,
				currency,
				balance_before_minor,
				balance_after_minor,
				created_at
			)
			VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9
			)
		`,
		entry.ID(),
		entry.WalletID(),
		entry.TransactionID(),
		string(entry.Direction()),
		entry.Value().MinorUnits(),
		entry.Value().Currency(),
		entry.BalanceBefore().MinorUnits(),
		entry.BalanceAfter().MinorUnits(),
		entry.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf(
			"insert ledger entry: %w",
			err,
		)
	}

	return nil
}

func (r *LedgerRepository) ListByWallet(
	ctx context.Context,
	walletID string,
	afterCreatedAt *time.Time,
	afterID string,
	limit int,
) (*LedgerPage, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if r.pool == nil {
		return nil, errors.New("ledger repository postgres pool is required for reads")
	}
	walletID = strings.TrimSpace(walletID)
	if walletID == "" {
		return nil, domain.ErrInvalidWalletID
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("ledger limit must be between 1 and 100")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, wallet_id::text, transaction_id::text, direction,
		       value_minor, currency, balance_before_minor, balance_after_minor, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		  AND ($2::timestamptz IS NULL OR (created_at, id) > ($2, $3::uuid))
		ORDER BY created_at ASC, id ASC
		LIMIT $4
	`, walletID, afterCreatedAt, nullableLedgerCursorID(afterCreatedAt, afterID), limit+1)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	defer rows.Close()

	records := make([]LedgerRecord, 0, limit+1)
	for rows.Next() {
		var id, wid, txid, direction, currency string
		var valueMinor, beforeMinor, afterMinor int64
		var createdAt time.Time
		if err := rows.Scan(&id, &wid, &txid, &direction, &valueMinor, &currency, &beforeMinor, &afterMinor, &createdAt); err != nil {
			return nil, fmt.Errorf("scan ledger page: %w", err)
		}
		value, err := domain.MoneyFromMinorUnits(valueMinor, currency)
		if err != nil {
			return nil, err
		}
		before, err := domain.MoneyFromMinorUnits(beforeMinor, currency)
		if err != nil {
			return nil, err
		}
		after, err := domain.MoneyFromMinorUnits(afterMinor, currency)
		if err != nil {
			return nil, err
		}
		records = append(records, LedgerRecord{
			ID: id, WalletID: wid, TransactionID: txid,
			Direction: domain.LedgerDirection(direction), Value: value,
			BalanceBefore: before, BalanceAfter: after, CreatedAt: createdAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger page: %w", err)
	}

	page := &LedgerPage{}
	if len(records) > limit {
		page.Entries = records[:limit]
		last := page.Entries[len(page.Entries)-1]
		page.NextCursor = fmt.Sprintf("%s|%s", last.CreatedAt.Format(time.RFC3339Nano), last.ID)
	} else {
		page.Entries = records
	}
	return page, nil
}

func (r *LedgerRepository) Reconcile(
	ctx context.Context,
	walletID string,
) (*ReconciliationSnapshot, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if r.pool == nil {
		return nil, errors.New("ledger repository postgres pool is required for reads")
	}
	walletID = strings.TrimSpace(walletID)
	if walletID == "" {
		return nil, domain.ErrInvalidWalletID
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currency string
	var storedMinor int64
	if err := tx.QueryRow(ctx, `SELECT currency, balance_minor FROM wallets WHERE id=$1`, walletID).Scan(&currency, &storedMinor); err != nil {
		return nil, err
	}

	var calculatedMinor int64
	var count int64
	if err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN direction='CREDIT' THEN value_minor ELSE -value_minor END), 0),
			COUNT(*)
		FROM ledger_entries
		WHERE wallet_id=$1
	`, walletID).Scan(&calculatedMinor, &count); err != nil {
		return nil, fmt.Errorf("calculate ledger balance: %w", err)
	}

	stored, err := domain.MoneyFromMinorUnits(storedMinor, currency)
	if err != nil {
		return nil, err
	}
	calculated, err := domain.MoneyFromMinorUnits(calculatedMinor, currency)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reconciliation snapshot: %w", err)
	}
	return &ReconciliationSnapshot{StoredBalance: stored, CalculatedBalance: calculated, CheckedEntries: count}, nil
}

func nullableLedgerCursorID(at *time.Time, id string) any {
	if at == nil {
		return nil
	}
	return strings.TrimSpace(id)
}
