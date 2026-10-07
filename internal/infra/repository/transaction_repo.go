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
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
)

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{pool: pool}
}

// Create inserts a wager transaction into the database within an active transaction.
func (r *TransactionRepository) Create(ctx context.Context, tx pgx.Tx, t *wager.Transaction) error {
	query := `
		INSERT INTO wager_transactions (
			id, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount_cents, currency,
			reference_external_transaction_id, resolved_reference_id, status, failure_code,
			balance_snapshot_cents, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
	var (
		failCodeStr *string
		snapCents   *int64
	)
	if t.FailureCode() != nil {
		s := string(*t.FailureCode())
		failCodeStr = &s
	}
	if t.BalanceSnapshot() != nil {
		c := t.BalanceSnapshot().Cents()
		snapCents = &c
	}

	_, err := tx.Exec(ctx, query,
		t.ID(),
		t.ProviderID(),
		t.ExternalTransactionID(),
		t.IdempotencyKey(),
		t.PayloadHash(),
		t.WalletID(),
		t.PlayerID(),
		t.RoundID(),
		t.GameID(),
		string(t.Kind()),
		t.Money().Cents(),
		t.Money().Currency(),
		t.ReferenceExternalTransactionID(),
		t.ResolvedReferenceID(),
		string(t.Status()),
		failCodeStr,
		snapCents,
		t.CreatedAt(),
		t.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert transaction: %w", err)
	}
	return nil
}

// Update updates the transaction state, failure code, balance snapshot, resolved reference, and updatedAt.
func (r *TransactionRepository) Update(ctx context.Context, tx pgx.Tx, t *wager.Transaction) error {
	query := `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, balance_snapshot_cents = $3,
		    resolved_reference_id = $4, updated_at = $5
		WHERE id = $6
	`
	var (
		failCodeStr *string
		snapCents   *int64
	)
	if t.FailureCode() != nil {
		s := string(*t.FailureCode())
		failCodeStr = &s
	}
	if t.BalanceSnapshot() != nil {
		c := t.BalanceSnapshot().Cents()
		snapCents = &c
	}

	cmdTag, err := tx.Exec(ctx, query,
		string(t.Status()),
		failCodeStr,
		snapCents,
		t.ResolvedReferenceID(),
		t.UpdatedAt(),
		t.ID(),
	)
	if err != nil {
		return fmt.Errorf("failed to update transaction: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}
	return nil
}

// FindByIdempotencyKey retrieves an existing transaction by its idempotency key.
func (r *TransactionRepository) FindByIdempotencyKey(ctx context.Context, key string) (*wager.Transaction, error) {
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount_cents, currency,
		       reference_external_transaction_id, resolved_reference_id, status, failure_code,
		       balance_snapshot_cents, created_at, updated_at
		FROM wager_transactions
		WHERE idempotency_key = $1
	`
	return r.scanRow(r.pool.QueryRow(ctx, query, key))
}

// FindByProviderAndExternalID retrieves a transaction by provider and external transaction ID.
func (r *TransactionRepository) FindByProviderAndExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount_cents, currency,
		       reference_external_transaction_id, resolved_reference_id, status, failure_code,
		       balance_snapshot_cents, created_at, updated_at
		FROM wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2
	`
	return r.scanRow(r.pool.QueryRow(ctx, query, providerID, externalID))
}

// FindByID retrieves a transaction by its internal UUID.
func (r *TransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount_cents, currency,
		       reference_external_transaction_id, resolved_reference_id, status, failure_code,
		       balance_snapshot_cents, created_at, updated_at
		FROM wager_transactions
		WHERE id = $1
	`
	return r.scanRow(r.pool.QueryRow(ctx, query, id))
}

// HasExistingReversal checks if a processed reversal already exists for this referenced transaction.
func (r *TransactionRepository) HasExistingReversal(ctx context.Context, providerID, refExternalID string, kind wager.Kind) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM wager_transactions
		WHERE provider_id = $1
		  AND reference_external_transaction_id = $2
		  AND kind = $3
		  AND status = 'PROCESSED'
	`
	var count int
	err := r.pool.QueryRow(ctx, query, providerID, refExternalID, string(kind)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// FindPendingReferences lists transactions waiting for referenced transactions.
func (r *TransactionRepository) FindPendingReferences(ctx context.Context, limit int) ([]*wager.Transaction, error) {
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount_cents, currency,
		       reference_external_transaction_id, resolved_reference_id, status, failure_code,
		       balance_snapshot_cents, created_at, updated_at
		FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE'
		ORDER BY created_at ASC
		LIMIT $1
	`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending references: %w", err)
	}
	defer rows.Close()

	var txs []*wager.Transaction
	for rows.Next() {
		tx, err := r.scanFromRows(rows)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *TransactionRepository) scanRow(row pgx.Row) (*wager.Transaction, error) {
	var (
		id, walletID, playerID           uuid.UUID
		providerID, extTxID, idemKey     string
		payloadHash                      string
		roundID, gameID, kindStr, curr   string
		amountCents                      int64
		refExternalID                    *string
		resolvedRefID                    *uuid.UUID
		statusStr                        string
		failCodeStr                      *string
		balanceSnapshotCents             *int64
		createdAt, updatedAt             time.Time
	)

	err := row.Scan(
		&id, &providerID, &extTxID, &idemKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID, &kindStr, &amountCents, &curr,
		&refExternalID, &resolvedRefID, &statusStr, &failCodeStr,
		&balanceSnapshotCents, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("failed to scan transaction row: %w", err)
	}

	m, err := money.FromCents(amountCents, curr)
	if err != nil {
		return nil, err
	}

	var failCode *wager.FailureCode
	if failCodeStr != nil {
		fc := wager.FailureCode(*failCodeStr)
		failCode = &fc
	}

	var balanceSnapshot *money.Money
	if balanceSnapshotCents != nil {
		bs, _ := money.FromCents(*balanceSnapshotCents, curr)
		balanceSnapshot = &bs
	}

	return wager.Rehydrate(
		id, providerID, extTxID, idemKey, payloadHash,
		walletID, playerID, roundID, gameID, wager.Kind(kindStr),
		m, refExternalID, resolvedRefID, wager.Status(statusStr),
		failCode, balanceSnapshot, createdAt, updatedAt,
	), nil
}

func (r *TransactionRepository) scanFromRows(rows pgx.Rows) (*wager.Transaction, error) {
	return r.scanRow(rows)
}
