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

type WalletService struct {
	transactions *postgresinfra.TransactionManager
	wallets      *postgresinfra.WalletRepository
	wagers       *postgresinfra.WagerRepository
	ledger       *postgresinfra.LedgerRepository
	outbox       *postgresinfra.OutboxRepository
	newID        IDGenerator
	now          func() time.Time
}

func NewWalletService(
	transactions *postgresinfra.TransactionManager,
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	outbox *postgresinfra.OutboxRepository,
	newID IDGenerator,
	now func() time.Time,
) (*WalletService, error) {
	if transactions == nil || wallets == nil || wagers == nil || ledger == nil ||
		outbox == nil || newID == nil || now == nil {
		return nil, errors.New("wallet service dependencies are required")
	}
	return &WalletService{
		transactions: transactions, wallets: wallets, wagers: wagers,
		ledger: ledger, outbox: outbox, newID: newID, now: now,
	}, nil
}

func (s *WalletService) Open(
	ctx context.Context,
	playerID string,
	initialBalance domain.Money,
	correlationID string,
) (*domain.Wallet, error) {
	return s.open(ctx, "", playerID, initialBalance, correlationID)
}

func (s *WalletService) OpenForProvider(
	ctx context.Context,
	providerID string,
	playerID string,
	initialBalance domain.Money,
	correlationID string,
) (*domain.Wallet, error) {
	return s.open(ctx, providerID, playerID, initialBalance, correlationID)
}

func (s *WalletService) open(
	ctx context.Context,
	providerID string,
	playerID string,
	initialBalance domain.Money,
	correlationID string,
) (*domain.Wallet, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	now := s.now().UTC()

	var (
		wallet *domain.Wallet
		err    error
	)
	if providerID == "" {
		wallet, err = domain.NewWallet(s.newID(), playerID, initialBalance, now)
	} else {
		wallet, err = domain.NewWalletForProvider(s.newID(), providerID, playerID, initialBalance, now)
	}
	if err != nil {
		return nil, err
	}

	err = s.transactions.WithinTransaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.wallets.Insert(ctx, tx, wallet); err != nil {
			return err
		}
		if initialBalance.IsZero() {
			return nil
		}

		opening, err := domain.RehydrateWagerTransaction(domain.RehydrateWagerTransactionParams{
			ID: s.newID(), WalletID: wallet.ID(), PlayerID: wallet.PlayerID(),
			Kind: domain.WagerKindOpening, Money: initialBalance,
			Status: domain.WagerStatusProcessed, ResultBalance: &initialBalance,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := s.wagers.Insert(ctx, tx, opening); err != nil {
			return err
		}

		zero, err := domain.ZeroMoney(initialBalance.Currency())
		if err != nil {
			return err
		}
		entry, err := domain.NewLedgerEntry(
			s.newID(), wallet.ID(), opening.ID(), domain.LedgerDirectionCredit,
			initialBalance, zero, initialBalance, now,
		)
		if err != nil {
			return err
		}
		if err := s.ledger.Insert(ctx, tx, entry); err != nil {
			return err
		}

		processed, err := domain.NewEvent(
			s.newID(), domain.EventTypeWagerTransactionProcessed, opening.ID(),
			correlationID, opening.ID(), now, 1,
			domain.WagerTransactionProcessedData{
				TransactionID: opening.ID(), WalletID: wallet.ID(),
				PlayerID: wallet.PlayerID(), Kind: domain.WagerKindOpening,
				Money: domain.NewMoneySnapshot(initialBalance),
			},
		)
		if err != nil {
			return err
		}
		if err := s.outbox.Insert(ctx, tx, processed); err != nil {
			return err
		}

		changed, err := domain.NewEvent(
			s.newID(), domain.EventTypeWalletBalanceChanged, wallet.ID(),
			correlationID, opening.ID(), now, wallet.Version(),
			domain.WalletBalanceChangedData{
				WalletID: wallet.ID(), TransactionID: opening.ID(),
				Direction:     domain.LedgerDirectionCredit,
				Money:         domain.NewMoneySnapshot(initialBalance),
				BalanceBefore: domain.NewMoneySnapshot(zero),
				BalanceAfter:  domain.NewMoneySnapshot(initialBalance),
				WalletVersion: wallet.Version(),
			},
		)
		if err != nil {
			return err
		}
		if err := s.outbox.Insert(ctx, tx, changed); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("open wallet: %w", err)
	}
	return wallet, nil
}
