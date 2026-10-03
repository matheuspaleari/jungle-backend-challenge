package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

const (
	FailureCodeInsufficientFunds         = "INSUFFICIENT_FUNDS"
	FailureCodeReferenceMismatch         = "REFERENCE_MISMATCH"
	FailureCodeReferenceInvalid          = "REFERENCE_INVALID"
	FailureCodeAlreadyReversed           = "ALREADY_REVERSED"
	FailureCodeReversalInsufficientFunds = "REVERSAL_INSUFFICIENT_FUNDS"
)

var (
	ErrUnsupportedWagerKind        = errors.New("unsupported wager kind")
	ErrWagerWalletMismatch         = errors.New("wager wallet mismatch")
	ErrWagerPlayerMismatch         = errors.New("wager player mismatch")
	ErrWagerProviderMismatch       = errors.New("wager provider does not own wallet")
	ErrIdempotencyConflict         = errors.New("idempotency key reused with different payload")
	ErrExternalTransactionConflict = errors.New("external transaction id already used with a different idempotency key")
)

type IDGenerator func() string

type WagerProcessor struct {
	transactions *postgresinfra.TransactionManager
	wallets      *postgresinfra.WalletRepository
	wagers       *postgresinfra.WagerRepository
	ledger       *postgresinfra.LedgerRepository
	outbox       *postgresinfra.OutboxRepository
	newID        IDGenerator
	now          func() time.Time
}

type ProcessWagerResult struct {
	Transaction *domain.WagerTransaction
	Balance     domain.Money
	Replay      bool
}

func NewWagerProcessor(
	transactions *postgresinfra.TransactionManager,
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	outbox *postgresinfra.OutboxRepository,
	newID IDGenerator,
	now func() time.Time,
) (*WagerProcessor, error) {
	if transactions == nil {
		return nil, errors.New("transaction manager is required")
	}

	if wallets == nil {
		return nil, errors.New("wallet repository is required")
	}

	if wagers == nil {
		return nil, errors.New("wager repository is required")
	}

	if ledger == nil {
		return nil, errors.New("ledger repository is required")
	}

	if outbox == nil {
		return nil, errors.New("outbox repository is required")
	}

	if newID == nil {
		return nil, errors.New("id generator is required")
	}

	if now == nil {
		return nil, errors.New("clock is required")
	}

	return &WagerProcessor{
		transactions: transactions,
		wallets:      wallets,
		wagers:       wagers,
		ledger:       ledger,
		outbox:       outbox,
		newID:        newID,
		now:          now,
	}, nil
}

func (p *WagerProcessor) Process(
	ctx context.Context,
	transaction *domain.WagerTransaction,
	correlationID string,
) (*ProcessWagerResult, error) {
	if err := validateProcessInput(ctx, transaction, correlationID); err != nil {
		return nil, err
	}

	replay, err := p.findReplay(ctx, transaction)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}

	var result *ProcessWagerResult
	err = p.transactions.WithinTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		processed, processErr := p.processNew(txCtx, tx, transaction, correlationID)
		if processErr != nil {
			return processErr
		}
		result = processed
		return nil
	})
	if err != nil {
		// A unique constraint may have been won by another process after our
		// optimistic pre-check. Resolve the persisted identity only after the
		// failed transaction has rolled back.
		replay, replayErr := p.findReplay(ctx, transaction)
		if replayErr != nil {
			return nil, replayErr
		}
		if replay != nil {
			return replay, nil
		}

		externalConflict, conflictErr := p.findExternalConflict(ctx, transaction)
		if conflictErr != nil {
			return nil, conflictErr
		}
		if externalConflict {
			return nil, ErrExternalTransactionConflict
		}
		return nil, err
	}
	return result, nil
}

// ProcessInTransaction is the SQS/inbox entry point. The caller owns tx so the
// inbox receipt/completion and every durable financial effect can commit or
// roll back as one PostgreSQL transaction.
func (p *WagerProcessor) ProcessInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	correlationID string,
) (*ProcessWagerResult, error) {
	if tx == nil {
		return nil, errors.New("postgres transaction is required")
	}
	if err := validateProcessInput(ctx, transaction, correlationID); err != nil {
		return nil, err
	}

	replay, err := p.findReplayInTransaction(ctx, tx, transaction)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}

	externalConflict, err := p.findExternalConflictInTransaction(ctx, tx, transaction)
	if err != nil {
		return nil, err
	}
	if externalConflict {
		return nil, ErrExternalTransactionConflict
	}

	return p.processNew(ctx, tx, transaction, correlationID)
}

func validateProcessInput(ctx context.Context, transaction *domain.WagerTransaction, correlationID string) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if transaction == nil {
		return errors.New("wager transaction is required")
	}
	if strings.TrimSpace(correlationID) == "" {
		return errors.New("correlation id is required")
	}
	switch transaction.Kind() {
	case domain.WagerKindBet, domain.WagerKindWin, domain.WagerKindLoss, domain.WagerKindRefund, domain.WagerKindRollback:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedWagerKind, transaction.Kind())
	}
}

func (p *WagerProcessor) findReplayInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	incoming *domain.WagerTransaction,
) (*ProcessWagerResult, error) {
	persisted, err := p.wagers.GetByIdempotencyKeyTx(ctx, tx, incoming.ProviderID(), incoming.IdempotencyKey())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup wager idempotency in transaction: %w", err)
	}
	if persisted.PayloadHash() != incoming.PayloadHash() {
		return nil, ErrIdempotencyConflict
	}
	balance, ok := persisted.ResultBalance()
	if !ok {
		return nil, fmt.Errorf("persisted wager %s has no result balance", persisted.ID())
	}
	return &ProcessWagerResult{Transaction: persisted, Balance: balance, Replay: true}, nil
}

func (p *WagerProcessor) findExternalConflictInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	incoming *domain.WagerTransaction,
) (bool, error) {
	persisted, err := p.wagers.GetByProviderAndExternalIDTx(ctx, tx, incoming.ProviderID(), incoming.ExternalTransactionID())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("lookup external transaction identity in transaction: %w", err)
	}
	return persisted.IdempotencyKey() != incoming.IdempotencyKey(), nil
}

func (p *WagerProcessor) findReplay(
	ctx context.Context,
	incoming *domain.WagerTransaction,
) (*ProcessWagerResult, error) {
	persisted, err := p.wagers.GetByIdempotencyKey(
		ctx,
		incoming.ProviderID(),
		incoming.IdempotencyKey(),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"lookup wager idempotency: %w",
			err,
		)
	}

	if persisted.PayloadHash() != incoming.PayloadHash() {
		return nil, ErrIdempotencyConflict
	}

	balance, ok := persisted.ResultBalance()
	if !ok {
		return nil, fmt.Errorf(
			"persisted wager %s has no result balance",
			persisted.ID(),
		)
	}

	return &ProcessWagerResult{
		Transaction: persisted,
		Balance:     balance,
		Replay:      true,
	}, nil
}

func (p *WagerProcessor) findExternalConflict(
	ctx context.Context,
	incoming *domain.WagerTransaction,
) (bool, error) {
	persisted, err := p.wagers.GetByProviderAndExternalID(
		ctx,
		incoming.ProviderID(),
		incoming.ExternalTransactionID(),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("lookup external transaction identity: %w", err)
	}

	return persisted.IdempotencyKey() != incoming.IdempotencyKey(), nil
}

func (p *WagerProcessor) processNew(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	correlationID string,
) (*ProcessWagerResult, error) {
	// Lock the wallet before inserting the wager transaction.
	//
	// Every financial operation for the same wallet must acquire locks in the
	// same order. Inserting the wager first can make PostgreSQL hold index/FK
	// related locks before waiting for the wallet row, while another concurrent
	// transaction already owns the wallet row and later needs those resources.
	// That inverted lock order can produce a deadlock.
	wallet, err := p.wallets.GetByIDForUpdate(
		ctx,
		tx,
		transaction.WalletID(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"lock wallet: %w",
			err,
		)
	}

	if wallet.ProviderID() != "" && wallet.ProviderID() != transaction.ProviderID() {
		return nil, ErrWagerProviderMismatch
	}

	if wallet.PlayerID() != transaction.PlayerID() {
		return nil, ErrWagerPlayerMismatch
	}

	if wallet.ID() != transaction.WalletID() {
		return nil, ErrWagerWalletMismatch
	}

	if wallet.Currency() != transaction.Money().Currency() {
		return nil, domain.ErrCurrencyMismatch
	}

	// Persist the transaction only after the per-wallet lock is held. This
	// establishes a deterministic lock order for concurrent financial writes:
	// wallet -> wager -> ledger/outbox.
	if err := p.wagers.Insert(
		ctx,
		tx,
		transaction,
	); err != nil {
		return nil, err
	}

	if transaction.ReferenceExternalID() != "" {
		reference, referenceErr := p.wagers.GetByProviderAndExternalID(
			ctx,
			transaction.ProviderID(),
			transaction.ReferenceExternalID(),
		)
		if referenceErr != nil {
			if errors.Is(referenceErr, pgx.ErrNoRows) {
				return p.persistPendingReference(
					ctx,
					tx,
					transaction,
					wallet,
					correlationID,
				)
			}
			return nil, fmt.Errorf("lookup wager reference: %w", referenceErr)
		}

		if reference.Status() == domain.WagerStatusPending ||
			reference.Status() == domain.WagerStatusPendingReference {
			return p.persistPendingReference(
				ctx, tx, transaction, wallet, correlationID,
			)
		}

		if reference.Status() != domain.WagerStatusProcessed {
			return p.rejectReference(
				ctx, tx, transaction, wallet, correlationID,
				FailureCodeReferenceInvalid,
			)
		}

		if err := p.validateReference(transaction, reference); err != nil {
			return p.rejectReference(
				ctx,
				tx,
				transaction,
				wallet,
				correlationID,
				FailureCodeReferenceMismatch,
			)
		}

		if err := transaction.MarkPendingReference(p.now().UTC()); err != nil {
			return nil, err
		}
		if err := transaction.ResolveReference(reference.ID(), p.now().UTC()); err != nil {
			return nil, err
		}

		if transaction.Kind() == domain.WagerKindRefund ||
			transaction.Kind() == domain.WagerKindRollback {
			reversed, err := p.wagers.HasProcessedReversal(ctx, tx, reference.ID())
			if err != nil {
				return nil, err
			}
			if reversed {
				return p.rejectReference(
					ctx,
					tx,
					transaction,
					wallet,
					correlationID,
					FailureCodeAlreadyReversed,
				)
			}
		}

		return p.processReferenced(
			ctx,
			tx,
			transaction,
			reference,
			wallet,
			correlationID,
		)
	}

	switch transaction.Kind() {
	case domain.WagerKindBet:
		return p.processBet(
			ctx,
			tx,
			transaction,
			wallet,
			correlationID,
		)

	case domain.WagerKindWin:
		return p.processWin(
			ctx,
			tx,
			transaction,
			wallet,
			correlationID,
		)

	case domain.WagerKindLoss:
		return p.processLoss(
			ctx,
			tx,
			transaction,
			wallet,
			correlationID,
		)

	case domain.WagerKindRefund,
		domain.WagerKindRollback:
		return nil, domain.ErrReferenceRequired

	default:
		return nil, ErrUnsupportedWagerKind
	}
}

func (p *WagerProcessor) processBet(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	balanceBefore := wallet.Balance()

	if err := wallet.Debit(
		transaction.Money(),
		now,
	); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			return p.rejectInsufficientFunds(
				ctx,
				tx,
				transaction,
				wallet,
				correlationID,
				now,
			)
		}

		return nil, err
	}

	if err := p.wallets.Update(
		ctx,
		tx,
		wallet,
	); err != nil {
		return nil, err
	}

	entry, err := domain.NewLedgerEntry(
		p.newID(),
		wallet.ID(),
		transaction.ID(),
		domain.LedgerDirectionDebit,
		transaction.Money(),
		balanceBefore,
		wallet.Balance(),
		now,
	)
	if err != nil {
		return nil, err
	}

	if err := p.ledger.Insert(
		ctx,
		tx,
		entry,
	); err != nil {
		return nil, err
	}

	if err := transaction.SetResultBalance(
		wallet.Balance(),
	); err != nil {
		return nil, err
	}

	if err := transaction.MarkProcessed(now); err != nil {
		return nil, err
	}

	if err := p.wagers.Update(
		ctx,
		tx,
		transaction,
	); err != nil {
		return nil, err
	}

	if err := p.insertProcessedEvents(
		ctx,
		tx,
		transaction,
		wallet,
		balanceBefore,
		domain.LedgerDirectionDebit,
		correlationID,
		now,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) processWin(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	balanceBefore := wallet.Balance()

	if err := wallet.Credit(
		transaction.Money(),
		now,
	); err != nil {
		return nil, err
	}

	if err := p.wallets.Update(
		ctx,
		tx,
		wallet,
	); err != nil {
		return nil, err
	}

	entry, err := domain.NewLedgerEntry(
		p.newID(),
		wallet.ID(),
		transaction.ID(),
		domain.LedgerDirectionCredit,
		transaction.Money(),
		balanceBefore,
		wallet.Balance(),
		now,
	)
	if err != nil {
		return nil, err
	}

	if err := p.ledger.Insert(
		ctx,
		tx,
		entry,
	); err != nil {
		return nil, err
	}

	if err := transaction.SetResultBalance(
		wallet.Balance(),
	); err != nil {
		return nil, err
	}

	if err := transaction.MarkProcessed(now); err != nil {
		return nil, err
	}

	if err := p.wagers.Update(
		ctx,
		tx,
		transaction,
	); err != nil {
		return nil, err
	}

	if err := p.insertProcessedEvents(
		ctx,
		tx,
		transaction,
		wallet,
		balanceBefore,
		domain.LedgerDirectionCredit,
		correlationID,
		now,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) processLoss(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()

	if err := transaction.SetResultBalance(
		wallet.Balance(),
	); err != nil {
		return nil, err
	}

	if err := transaction.MarkProcessed(now); err != nil {
		return nil, err
	}

	if err := p.wagers.Update(
		ctx,
		tx,
		transaction,
	); err != nil {
		return nil, err
	}

	event, err := domain.NewEvent(
		p.newID(),
		domain.EventTypeWagerTransactionProcessed,
		transaction.ID(),
		correlationID,
		transaction.ID(),
		now,
		1,
		domain.WagerTransactionProcessedData{
			TransactionID:         transaction.ID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			ProviderID:            transaction.ProviderID(),
			WalletID:              transaction.WalletID(),
			PlayerID:              transaction.PlayerID(),
			Kind:                  transaction.Kind(),
			Money: domain.NewMoneySnapshot(
				transaction.Money(),
			),
		},
	)
	if err != nil {
		return nil, err
	}

	if err := p.outbox.Insert(
		ctx,
		tx,
		event,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) rejectInsufficientFunds(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
	now time.Time,
) (*ProcessWagerResult, error) {
	if err := transaction.SetResultBalance(
		wallet.Balance(),
	); err != nil {
		return nil, err
	}

	if err := transaction.MarkRejected(
		FailureCodeInsufficientFunds,
		now,
	); err != nil {
		return nil, err
	}

	if err := p.wagers.Update(
		ctx,
		tx,
		transaction,
	); err != nil {
		return nil, err
	}

	event, err := domain.NewEvent(
		p.newID(),
		domain.EventTypeWagerTransactionRejected,
		transaction.ID(),
		correlationID,
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
			FailureCode:           FailureCodeInsufficientFunds,
		},
	)
	if err != nil {
		return nil, err
	}

	if err := p.outbox.Insert(
		ctx,
		tx,
		event,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) insertProcessedEvents(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	balanceBefore domain.Money,
	direction domain.LedgerDirection,
	correlationID string,
	now time.Time,
) error {
	processedEvent, err := domain.NewEvent(
		p.newID(),
		domain.EventTypeWagerTransactionProcessed,
		transaction.ID(),
		correlationID,
		transaction.ID(),
		now,
		1,
		domain.WagerTransactionProcessedData{
			TransactionID:         transaction.ID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			ProviderID:            transaction.ProviderID(),
			WalletID:              transaction.WalletID(),
			PlayerID:              transaction.PlayerID(),
			Kind:                  transaction.Kind(),
			Money: domain.NewMoneySnapshot(
				transaction.Money(),
			),
		},
	)
	if err != nil {
		return err
	}

	if err := p.outbox.Insert(
		ctx,
		tx,
		processedEvent,
	); err != nil {
		return err
	}

	balanceEvent, err := domain.NewEvent(
		p.newID(),
		domain.EventTypeWalletBalanceChanged,
		wallet.ID(),
		correlationID,
		transaction.ID(),
		now,
		wallet.Version(),
		domain.WalletBalanceChangedData{
			WalletID:      wallet.ID(),
			TransactionID: transaction.ID(),
			Direction:     direction,
			Money: domain.NewMoneySnapshot(
				transaction.Money(),
			),
			BalanceBefore: domain.NewMoneySnapshot(
				balanceBefore,
			),
			BalanceAfter: domain.NewMoneySnapshot(
				wallet.Balance(),
			),
			WalletVersion: wallet.Version(),
		},
	)
	if err != nil {
		return err
	}

	if err := p.outbox.Insert(
		ctx,
		tx,
		balanceEvent,
	); err != nil {
		return err
	}

	return nil
}

func (p *WagerProcessor) validateReference(
	transaction *domain.WagerTransaction,
	reference *domain.WagerTransaction,
) error {
	if reference.Status() != domain.WagerStatusProcessed {
		return errors.New("reference transaction is not processed")
	}
	if reference.ProviderID() != transaction.ProviderID() {
		return ErrWagerPlayerMismatch
	}
	if reference.PlayerID() != transaction.PlayerID() {
		return ErrWagerPlayerMismatch
	}
	if reference.WalletID() != transaction.WalletID() {
		return ErrWagerWalletMismatch
	}
	if reference.Money().Currency() != transaction.Money().Currency() {
		return domain.ErrCurrencyMismatch
	}
	if reference.RoundID() != transaction.RoundID() {
		return errors.New("reference round mismatch")
	}
	if !reference.Money().Equal(transaction.Money()) {
		return errors.New("reference amount mismatch")
	}

	switch transaction.Kind() {
	case domain.WagerKindWin:
		if reference.Kind() != domain.WagerKindBet {
			return errors.New("referenced WIN must reference BET")
		}
	case domain.WagerKindRefund:
		if reference.Kind() != domain.WagerKindBet {
			return errors.New("REFUND must reference BET")
		}
	case domain.WagerKindRollback:
		switch reference.Kind() {
		case domain.WagerKindBet, domain.WagerKindWin, domain.WagerKindRefund:
		default:
			return errors.New("ROLLBACK reference kind is not reversible")
		}
	}
	return nil
}

func (p *WagerProcessor) persistPendingReference(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	if err := transaction.SetResultBalance(wallet.Balance()); err != nil {
		return nil, err
	}
	if err := transaction.MarkPendingReference(now); err != nil {
		return nil, err
	}

	if err := p.wagers.UpdatePendingReference(
		ctx,
		tx,
		transaction,
		postgresinfra.PendingReferenceSchedule{
			AttemptCount:  0,
			NextAttemptAt: now.Add(5 * time.Second),
			ExpiresAt:     now.Add(24 * time.Hour),
		},
	); err != nil {
		return nil, err
	}

	event, err := domain.NewEvent(
		p.newID(),
		domain.EventTypePendingReference,
		transaction.ID(),
		correlationID,
		transaction.ID(),
		now,
		1,
		domain.PendingReferenceData{
			TransactionID:         transaction.ID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			ProviderID:            transaction.ProviderID(),
			WalletID:              transaction.WalletID(),
			Kind:                  transaction.Kind(),
			ReferenceExternalID:   transaction.ReferenceExternalID(),
		},
	)
	if err != nil {
		return nil, err
	}
	if err := p.outbox.Insert(ctx, tx, event); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) rejectReference(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
	failureCode string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	if err := transaction.SetResultBalance(wallet.Balance()); err != nil {
		return nil, err
	}
	if err := transaction.MarkRejected(failureCode, now); err != nil {
		return nil, err
	}
	if err := p.wagers.Update(ctx, tx, transaction); err != nil {
		return nil, err
	}

	event, err := domain.NewEvent(
		p.newID(),
		domain.EventTypeWagerTransactionRejected,
		transaction.ID(),
		correlationID,
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
		return nil, err
	}
	if err := p.outbox.Insert(ctx, tx, event); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) processReferenced(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	reference *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	switch transaction.Kind() {
	case domain.WagerKindWin, domain.WagerKindRefund:
		return p.processCreditMovement(
			ctx, tx, transaction, wallet, correlationID,
		)
	case domain.WagerKindRollback:
		switch reference.Kind() {
		case domain.WagerKindBet:
			return p.processCreditMovement(
				ctx, tx, transaction, wallet, correlationID,
			)
		case domain.WagerKindWin, domain.WagerKindRefund:
			return p.processDebitMovement(
				ctx, tx, transaction, wallet, correlationID,
			)
		}
	}
	return nil, ErrUnsupportedWagerKind
}

func (p *WagerProcessor) processCreditMovement(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	before := wallet.Balance()

	if err := wallet.Credit(transaction.Money(), now); err != nil {
		return nil, err
	}
	if err := p.wallets.Update(ctx, tx, wallet); err != nil {
		return nil, err
	}

	entry, err := domain.NewLedgerEntry(
		p.newID(), wallet.ID(), transaction.ID(),
		domain.LedgerDirectionCredit,
		transaction.Money(), before, wallet.Balance(), now,
	)
	if err != nil {
		return nil, err
	}
	if err := p.ledger.Insert(ctx, tx, entry); err != nil {
		return nil, err
	}
	if err := transaction.SetResultBalance(wallet.Balance()); err != nil {
		return nil, err
	}
	if err := transaction.MarkProcessed(now); err != nil {
		return nil, err
	}
	if err := p.wagers.Update(ctx, tx, transaction); err != nil {
		return nil, err
	}
	if err := p.insertProcessedEvents(
		ctx, tx, transaction, wallet, before,
		domain.LedgerDirectionCredit, correlationID, now,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}

func (p *WagerProcessor) processDebitMovement(
	ctx context.Context,
	tx pgx.Tx,
	transaction *domain.WagerTransaction,
	wallet *domain.Wallet,
	correlationID string,
) (*ProcessWagerResult, error) {
	now := p.now().UTC()
	before := wallet.Balance()

	if err := wallet.Debit(transaction.Money(), now); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			if transaction.Kind() == domain.WagerKindRollback {
				return p.rejectReference(
					ctx, tx, transaction, wallet, correlationID,
					FailureCodeReversalInsufficientFunds,
				)
			}
			return p.rejectInsufficientFunds(
				ctx, tx, transaction, wallet, correlationID, now,
			)
		}
		return nil, err
	}
	if err := p.wallets.Update(ctx, tx, wallet); err != nil {
		return nil, err
	}

	entry, err := domain.NewLedgerEntry(
		p.newID(), wallet.ID(), transaction.ID(),
		domain.LedgerDirectionDebit,
		transaction.Money(), before, wallet.Balance(), now,
	)
	if err != nil {
		return nil, err
	}
	if err := p.ledger.Insert(ctx, tx, entry); err != nil {
		return nil, err
	}
	if err := transaction.SetResultBalance(wallet.Balance()); err != nil {
		return nil, err
	}
	if err := transaction.MarkProcessed(now); err != nil {
		return nil, err
	}
	if err := p.wagers.Update(ctx, tx, transaction); err != nil {
		return nil, err
	}
	if err := p.insertProcessedEvents(
		ctx, tx, transaction, wallet, before,
		domain.LedgerDirectionDebit, correlationID, now,
	); err != nil {
		return nil, err
	}

	return &ProcessWagerResult{
		Transaction: transaction,
		Balance:     wallet.Balance(),
		Replay:      false,
	}, nil
}
