package repository

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

// Insert appends an immutable ledger entry within the active SQL transaction.
func (r *LedgerRepository) Insert(ctx context.Context, tx pgx.Tx, entry *wallet.LedgerEntry) error {
	query := `
		INSERT INTO wallet_ledger (
			id, wallet_id, transaction_id, direction, amount_cents, currency,
			balance_before_cents, balance_after_cents, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, query,
		entry.ID(),
		entry.WalletID(),
		entry.TransactionID(),
		string(entry.Direction()),
		entry.Amount().Cents(),
		entry.Amount().Currency(),
		entry.BalanceBefore().Cents(),
		entry.BalanceAfter().Cents(),
		entry.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}
	return nil
}

// LedgerItemDTO for read queries.
type LedgerItemDTO struct {
	ID            uuid.UUID        `json:"id"`
	WalletID      uuid.UUID        `json:"walletId"`
	TransactionID uuid.UUID        `json:"transactionId"`
	Direction     wallet.Direction `json:"direction"`
	Amount        money.Money      `json:"amount"`
	BalanceBefore money.Money      `json:"balanceBefore"`
	BalanceAfter  money.Money      `json:"balanceAfter"`
	CreatedAt     time.Time        `json:"createdAt"`
}

// ListByWallet returns ledger items with opaque cursor pagination.
func (r *LedgerRepository) ListByWallet(
	ctx context.Context,
	walletID uuid.UUID,
	limit int,
	cursor string,
) ([]LedgerItemDTO, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var cursorTime time.Time
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			parsed, parseErr := time.Parse(time.RFC3339Nano, string(decoded))
			if parseErr == nil {
				cursorTime = parsed
			}
		}
	}

	query := `
		SELECT id, wallet_id, transaction_id, direction, amount_cents, currency,
		       balance_before_cents, balance_after_cents, created_at
		FROM wallet_ledger
		WHERE wallet_id = $1 AND ($2::timestamptz IS NULL OR created_at > $2)
		ORDER BY created_at ASC
		LIMIT $3
	`

	var cursorParam interface{}
	if !cursorTime.IsZero() {
		cursorParam = cursorTime
	} else {
		cursorParam = nil
	}

	rows, err := r.pool.Query(ctx, query, walletID, cursorParam, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list ledger: %w", err)
	}
	defer rows.Close()

	var items []LedgerItemDTO
	for rows.Next() {
		var (
			id, wID, txID                        uuid.UUID
			dir                                  string
			amountCents, beforeCents, afterCents int64
			currency                             string
			createdAt                            time.Time
		)
		if err := rows.Scan(&id, &wID, &txID, &dir, &amountCents, &currency, &beforeCents, &afterCents, &createdAt); err != nil {
			return nil, "", err
		}

		amt, _ := money.FromCents(amountCents, currency)
		before, _ := money.FromCents(beforeCents, currency)
		after, _ := money.FromCents(afterCents, currency)

		items = append(items, LedgerItemDTO{
			ID:            id,
			WalletID:      wID,
			TransactionID: txID,
			Direction:     wallet.Direction(dir),
			Amount:        amt,
			BalanceBefore: before,
			BalanceAfter:  after,
			CreatedAt:     createdAt,
		})
	}

	var nextCursor string
	if len(items) > limit {
		nextItem := items[limit]
		items = items[:limit]
		nextCursor = base64.StdEncoding.EncodeToString([]byte(nextItem.CreatedAt.Format(time.RFC3339Nano)))
	}

	return items, nextCursor, nil
}

// CalculateReconciliation sums credits and debits to reconstruct the exact wallet balance.
func (r *LedgerRepository) CalculateReconciliation(
	ctx context.Context,
	walletID uuid.UUID,
	currency string,
) (calculated money.Money, count int, err error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_cents ELSE 0 END), 0) AS total_credits,
			COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount_cents ELSE 0 END), 0) AS total_debits,
			COUNT(*) AS total_count
		FROM wallet_ledger
		WHERE wallet_id = $1
	`
	var credits, debits int64
	err = r.pool.QueryRow(ctx, query, walletID).Scan(&credits, &debits, &count)
	if err != nil {
		return money.Money{}, 0, fmt.Errorf("failed to calculate reconciliation: %w", err)
	}

	netCents := credits - debits
	calculated, err = money.FromCents(netCents, currency)
	if err != nil {
		return money.Money{}, 0, err
	}

	return calculated, count, nil
}
