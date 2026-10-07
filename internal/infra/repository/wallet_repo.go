package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
)

var (
	ErrWalletNotFound = errors.New("wallet not found")
	ErrWalletConflict = errors.New("wallet already exists for player and currency")
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

// Create inserts a new wallet aggregate.
func (r *WalletRepository) Create(ctx context.Context, tx pgx.Tx, w *wallet.Wallet) error {
	query := `
		INSERT INTO wallets (id, player_id, currency, balance_cents, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(ctx, query,
		w.ID(),
		w.PlayerID(),
		w.Currency(),
		w.Balance().Cents(),
		w.Version(),
		w.CreatedAt(),
		w.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert wallet: %w", err)
	}
	return nil
}

// FindByID retrieves a wallet without locks.
func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	query := `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	var (
		wID          uuid.UUID
		playerID     uuid.UUID
		currency     string
		balanceCents int64
		version      int64
		createdAt    time.Time
		updatedAt    time.Time
	)

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&wID, &playerID, &currency, &balanceCents, &version, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet: %w", err)
	}

	bal, err := money.FromCents(balanceCents, currency)
	if err != nil {
		return nil, err
	}

	return wallet.Rehydrate(wID, playerID, currency, bal, version, createdAt, updatedAt)
}

// FindByIDForUpdate acquires a pessimistic row-level lock on the wallet (SELECT FOR UPDATE).
// CRITICAL: Guarantees that concurrent transactions on the same wallet are serialized at the database level,
// preventing lost updates and race conditions, while different wallets execute concurrently in parallel.
func (r *WalletRepository) FindByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*wallet.Wallet, error) {
	query := `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`
	var (
		wID          uuid.UUID
		playerID     uuid.UUID
		currency     string
		balanceCents int64
		version      int64
		createdAt    time.Time
		updatedAt    time.Time
	)

	err := tx.QueryRow(ctx, query, id).Scan(
		&wID, &playerID, &currency, &balanceCents, &version, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet for update: %w", err)
	}

	bal, err := money.FromCents(balanceCents, currency)
	if err != nil {
		return nil, err
	}

	return wallet.Rehydrate(wID, playerID, currency, bal, version, createdAt, updatedAt)
}

// FindByPlayerAndCurrency finds a wallet by player ID and currency.
func (r *WalletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*wallet.Wallet, error) {
	query := `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets
		WHERE player_id = $1 AND currency = $2
	`
	var (
		wID          uuid.UUID
		pID          uuid.UUID
		curr         string
		balanceCents int64
		version      int64
		createdAt    time.Time
		updatedAt    time.Time
	)

	err := r.pool.QueryRow(ctx, query, playerID, currency).Scan(
		&wID, &pID, &curr, &balanceCents, &version, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet by player and currency: %w", err)
	}

	bal, err := money.FromCents(balanceCents, curr)
	if err != nil {
		return nil, err
	}

	return wallet.Rehydrate(wID, pID, curr, bal, version, createdAt, updatedAt)
}

// UpdateBalance updates the balance, version, and updatedAt of a locked wallet.
func (r *WalletRepository) UpdateBalance(ctx context.Context, tx pgx.Tx, w *wallet.Wallet) error {
	query := `
		UPDATE wallets
		SET balance_cents = $1, version = $2, updated_at = $3
		WHERE id = $4
	`
	cmdTag, err := tx.Exec(ctx, query, w.Balance().Cents(), w.Version(), w.UpdatedAt(), w.ID())
	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrWalletNotFound
	}
	return nil
}
