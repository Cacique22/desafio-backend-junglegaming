package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/events"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
)

var (
	ErrWalletAlreadyExists = errors.New("wallet already exists for player and currency")
)

type WalletUseCase struct {
	pool       *pgxpool.Pool
	walletRepo *repository.WalletRepository
	ledgerRepo *repository.LedgerRepository
	txRepo     *repository.TransactionRepository
	outboxRepo *repository.OutboxRepository
}

func NewWalletUseCase(
	pool *pgxpool.Pool,
	walletRepo *repository.WalletRepository,
	ledgerRepo *repository.LedgerRepository,
	txRepo *repository.TransactionRepository,
	outboxRepo *repository.OutboxRepository,
) *WalletUseCase {
	return &WalletUseCase{
		pool:       pool,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		txRepo:     txRepo,
		outboxRepo: outboxRepo,
	}
}

type CreateWalletInput struct {
	PlayerID       uuid.UUID   `json:"playerId"`
	InitialBalance money.Money `json:"initialBalance"`
}

type WalletOutput struct {
	ID        uuid.UUID   `json:"id"`
	PlayerID  uuid.UUID   `json:"playerId"`
	Balance   money.Money `json:"balance"`
	Version   int64       `json:"version"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// CreateWallet provisions a new wallet with atomic initial balance handling.
func (uc *WalletUseCase) CreateWallet(ctx context.Context, input CreateWalletInput) (*WalletOutput, error) {
	// Check existing wallet for this player and currency
	existing, err := uc.walletRepo.FindByPlayerAndCurrency(ctx, input.PlayerID, input.InitialBalance.Currency())
	if err == nil && existing != nil {
		return nil, ErrWalletAlreadyExists
	}
	if err != nil && !errors.Is(err, repository.ErrWalletNotFound) {
		return nil, fmt.Errorf("failed to check existing wallet: %w", err)
	}

	w, err := wallet.New(input.PlayerID, input.InitialBalance)
	if err != nil {
		return nil, err
	}

	tx, err := uc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := uc.walletRepo.Create(ctx, tx, w); err != nil {
		return nil, err
	}

	// If initial balance > 0, create OPENING transaction, ledger entry, and outbox events atomically
	if input.InitialBalance.IsPositive() {
		openingTx, err := wager.NewInternalOpening(w.ID(), w.PlayerID(), input.InitialBalance)
		if err != nil {
			return nil, err
		}

		if err := uc.txRepo.Create(ctx, tx, openingTx); err != nil {
			return nil, err
		}

		zeroMoney, _ := money.Zero(w.Currency())
		ledgerEntry, err := wallet.NewLedgerEntry(
			w.ID(),
			openingTx.ID(),
			wallet.DirectionCredit,
			input.InitialBalance,
			zeroMoney,
			input.InitialBalance,
		)
		if err != nil {
			return nil, err
		}

		if err := uc.ledgerRepo.Insert(ctx, tx, ledgerEntry); err != nil {
			return nil, err
		}

		// Outbox: WagerTransactionProcessed
		finBalStr := input.InitialBalance.String()
		txProcEvt, err := events.NewEnvelope(
			events.TypeWagerTransactionProcessed,
			openingTx.ID().String(),
			openingTx.ID().String(),
			nil,
			1,
			events.WagerTransactionProcessedPayload{
				TransactionID: openingTx.ID().String(),
				ProviderID:    "SYSTEM",
				PlayerID:      w.PlayerID().String(),
				WalletID:      w.ID().String(),
				RoundID:       "OPENING",
				GameID:        "SYSTEM",
				Kind:          string(wager.KindOpening),
				Money:         input.InitialBalance,
				Status:        string(wager.StatusProcessed),
				FinalBalance:  &finBalStr,
			},
		)
		if err != nil {
			return nil, err
		}
		if err := uc.outboxRepo.Insert(ctx, tx, txProcEvt); err != nil {
			return nil, err
		}

		// Outbox: WalletBalanceChanged
		balChangeEvt, err := events.NewEnvelope(
			events.TypeWalletBalanceChanged,
			w.ID().String(),
			openingTx.ID().String(),
			nil,
			1,
			events.WalletBalanceChangedPayload{
				WalletID:      w.ID().String(),
				TransactionID: openingTx.ID().String(),
				Direction:     string(wallet.DirectionCredit),
				Money:         input.InitialBalance,
				BalanceBefore: "0.00",
				BalanceAfter:  input.InitialBalance.String(),
				WalletVersion: w.Version(),
			},
		)
		if err != nil {
			return nil, err
		}
		if err := uc.outboxRepo.Insert(ctx, tx, balChangeEvt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit wallet creation: %w", err)
	}

	return &WalletOutput{
		ID:        w.ID(),
		PlayerID:  w.PlayerID(),
		Balance:   w.Balance(),
		Version:   w.Version(),
		CreatedAt: w.CreatedAt(),
		UpdatedAt: w.UpdatedAt(),
	}, nil
}

// GetWallet retrieves a wallet by its ID.
func (uc *WalletUseCase) GetWallet(ctx context.Context, id uuid.UUID) (*WalletOutput, error) {
	w, err := uc.walletRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &WalletOutput{
		ID:        w.ID(),
		PlayerID:  w.PlayerID(),
		Balance:   w.Balance(),
		Version:   w.Version(),
		CreatedAt: w.CreatedAt(),
		UpdatedAt: w.UpdatedAt(),
	}, nil
}

type ReconciliationOutput struct {
	WalletID          uuid.UUID   `json:"walletId"`
	StoredBalance     money.Money `json:"storedBalance"`
	CalculatedBalance money.Money `json:"calculatedBalance"`
	Difference        money.Money `json:"difference"`
	Consistent        bool        `json:"consistent"`
	CheckedEntries    int         `json:"checkedEntries"`
}

// ReconcileWallet rebuilds the wallet balance from the ledger and audits for divergence.
func (uc *WalletUseCase) ReconcileWallet(ctx context.Context, walletID uuid.UUID) (*ReconciliationOutput, error) {
	w, err := uc.walletRepo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	calcBal, count, err := uc.ledgerRepo.CalculateReconciliation(ctx, walletID, w.Currency())
	if err != nil {
		return nil, err
	}

	diff, err := w.Balance().Sub(calcBal)
	if err != nil {
		return nil, err
	}

	return &ReconciliationOutput{
		WalletID:          walletID,
		StoredBalance:     w.Balance(),
		CalculatedBalance: calcBal,
		Difference:        diff,
		Consistent:        diff.IsZero(),
		CheckedEntries:    count,
	}, nil
}
