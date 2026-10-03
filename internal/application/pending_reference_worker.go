package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

const (
	FailureCodeReferenceExpired        = "REFERENCE_EXPIRED"
	FailureCodeReferenceRetryExhausted = "REFERENCE_RETRY_EXHAUSTED"
	PendingReferenceMaxAttempts        = 8
)

type PendingReferenceWorker struct {
	txManager *postgresinfra.TransactionManager
	wallets   *postgresinfra.WalletRepository
	wagers    *postgresinfra.WagerRepository
	ledger    *postgresinfra.LedgerRepository
	outbox    *postgresinfra.OutboxRepository
	newID     IDGenerator
	now       func() time.Time
	batchSize int
}

func NewPendingReferenceWorker(
	txManager *postgresinfra.TransactionManager,
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	outbox *postgresinfra.OutboxRepository,
	newID IDGenerator,
	now func() time.Time,
	batchSize int,
) (*PendingReferenceWorker, error) {
	if txManager == nil || wallets == nil || wagers == nil ||
		ledger == nil || outbox == nil || newID == nil || now == nil {
		return nil, errors.New("pending reference worker dependencies are required")
	}
	if batchSize <= 0 {
		return nil, errors.New("pending reference worker batch size must be positive")
	}
	return &PendingReferenceWorker{
		txManager: txManager, wallets: wallets, wagers: wagers,
		ledger: ledger, outbox: outbox, newID: newID, now: now,
		batchSize: batchSize,
	}, nil
}

func (w *PendingReferenceWorker) RunOnce(ctx context.Context) (int, error) {
	if ctx == nil {
		return 0, errors.New("context is required")
	}

	processed := 0
	for processed < w.batchSize {
		handled := false
		err := w.txManager.WithinTransaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
			now := w.now().UTC()
			item, err := w.wagers.ClaimDuePendingReference(ctx, tx, now)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			handled = true
			return w.handleClaimed(ctx, tx, item, now)
		})
		if err != nil {
			return processed, err
		}
		if !handled {
			break
		}
		processed++
	}
	return processed, nil
}

func (w *PendingReferenceWorker) handleClaimed(
	ctx context.Context,
	tx pgx.Tx,
	item *postgresinfra.PendingReferenceRecord,
	now time.Time,
) error {
	transaction := item.Transaction

	if !now.Before(item.ExpiresAt) {
		return w.rejectExpired(ctx, tx, transaction, now)
	}

	reference, err := w.wagers.GetByProviderAndExternalIDTx(
		ctx, tx, transaction.ProviderID(), transaction.ReferenceExternalID(),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return w.reschedule(ctx, tx, item, now)
		}
		return fmt.Errorf("lookup pending reference: %w", err)
	}

	processor := &WagerProcessor{
		transactions: w.txManager,
		wallets:      w.wallets,
		wagers:       w.wagers,
		ledger:       w.ledger,
		outbox:       w.outbox,
		newID:        w.newID,
		now:          w.now,
	}

	if reference.Status() == domain.WagerStatusPending ||
		reference.Status() == domain.WagerStatusPendingReference {
		return w.reschedule(ctx, tx, item, now)
	}

	if reference.Status() != domain.WagerStatusProcessed {
		return w.rejectPendingReference(
			ctx, tx, transaction, now, FailureCodeReferenceInvalid,
		)
	}

	if err := processor.validateReference(transaction, reference); err != nil {
		return w.rejectPendingReference(
			ctx, tx, transaction, now, FailureCodeReferenceMismatch,
		)
	}

	if err := transaction.ResolveReference(reference.ID(), now); err != nil {
		return err
	}

	wallet, err := w.wallets.GetByIDForUpdate(ctx, tx, transaction.WalletID())
	if err != nil {
		return fmt.Errorf("lock pending reference wallet: %w", err)
	}

	if transaction.Kind() == domain.WagerKindRefund ||
		transaction.Kind() == domain.WagerKindRollback {
		reversed, err := w.wagers.HasProcessedReversal(ctx, tx, reference.ID())
		if err != nil {
			return err
		}
		if reversed {
			_, err := processor.rejectReference(
				ctx, tx, transaction, wallet,
				"pending-reference-"+transaction.ID(),
				FailureCodeAlreadyReversed,
			)
			return err
		}
	}

	_, err = processor.processReferenced(
		ctx, tx, transaction, reference, wallet,
		"pending-reference-"+transaction.ID(),
	)
	return err
}

func (w *PendingReferenceWorker) reschedule(
	ctx context.Context,
	tx pgx.Tx,
	item *postgresinfra.PendingReferenceRecord,
	now time.Time,
) error {
	attempt := item.AttemptCount + 1
	if attempt >= PendingReferenceMaxAttempts {
		return w.rejectPendingReference(
			ctx, tx, item.Transaction, now, FailureCodeReferenceRetryExhausted,
		)
	}

	next := now.Add(referenceBackoff(attempt))
	if next.After(item.ExpiresAt) {
		next = item.ExpiresAt
	}
	return w.wagers.ReschedulePendingReference(
		ctx, tx, item.Transaction.ID(), attempt, next, now,
	)
}

func referenceBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	return 5 * time.Second * time.Duration(1<<shift)
}

func (w *PendingReferenceWorker) rejectExpired(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	now time.Time,
) error {
	return w.rejectPendingReference(
		ctx, tx, transaction, now, FailureCodeReferenceExpired,
	)
}

func (w *PendingReferenceWorker) rejectPendingReference(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	now time.Time,
	failureCode string,
) error {
	wallet, err := w.wallets.GetByIDForUpdate(ctx, tx, transaction.WalletID())
	if err != nil {
		return err
	}

	if err := transaction.SetResultBalance(wallet.Balance()); err != nil {
		return err
	}
	if err := transaction.MarkRejected(failureCode, now); err != nil {
		return err
	}
	if err := w.wagers.Update(ctx, tx, transaction); err != nil {
		return err
	}

	event, err := domain.NewEvent(
		w.newID(),
		domain.EventTypeWagerTransactionRejected,
		transaction.ID(),
		"pending-reference-"+transaction.ID(),
		transaction.ID(),
		now,
		1,
		domain.WagerTransactionRejectedData{
			TransactionID:         transaction.ID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			ProviderID:            transaction.ProviderID(),
			WalletID:              transaction.WalletID(),
			PlayerID:              transaction.PlayerID(),
			Kind:                  transaction.Kind(),
			FailureCode:           failureCode,
		},
	)
	if err != nil {
		return err
	}

	return w.outbox.Insert(ctx, tx, event)
}
