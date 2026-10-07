package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInboxDuplicateCompleted = errors.New("inbox message has already been processed")
	ErrInboxPayloadMismatch    = errors.New("inbox message re-delivered with different payload hash")
)

type InboxRepository struct {
	pool *pgxpool.Pool
}

func NewInboxRepository(pool *pgxpool.Pool) *InboxRepository {
	return &InboxRepository{pool: pool}
}

// RecordOrCheck checks if a message has already been processed by consumerName.
// If it's a new message, inserts it within tx.
// If it was already completed, returns ErrInboxDuplicateCompleted.
func (r *InboxRepository) RecordOrCheck(
	ctx context.Context,
	tx pgx.Tx,
	consumerName string,
	messageID string,
	payloadHash string,
) error {
	queryCheck := `
		SELECT payload_hash, completed_at
		FROM inbox
		WHERE consumer_name = $1 AND message_id = $2
		FOR UPDATE
	`
	var existingHash string
	var completedAt *time.Time

	err := tx.QueryRow(ctx, queryCheck, consumerName, messageID).Scan(&existingHash, &completedAt)
	if err == nil {
		if existingHash != payloadHash {
			return ErrInboxPayloadMismatch
		}
		if completedAt != nil {
			return ErrInboxDuplicateCompleted
		}
		// Existing but not yet completed (e.g. previous crash before commit)
		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to check inbox: %w", err)
	}

	// Not found, insert new
	queryInsert := `
		INSERT INTO inbox (consumer_name, message_id, payload_hash, received_at)
		VALUES ($1, $2, $3, NOW())
	`
	_, err = tx.Exec(ctx, queryInsert, consumerName, messageID, payloadHash)
	if err != nil {
		return fmt.Errorf("failed to insert inbox: %w", err)
	}

	return nil
}

// MarkCompleted marks the inbox message as completed within tx.
func (r *InboxRepository) MarkCompleted(
	ctx context.Context,
	tx pgx.Tx,
	consumerName string,
	messageID string,
) error {
	query := `
		UPDATE inbox
		SET completed_at = NOW()
		WHERE consumer_name = $1 AND message_id = $2
	`
	_, err := tx.Exec(ctx, query, consumerName, messageID)
	if err != nil {
		return fmt.Errorf("failed to mark inbox completed: %w", err)
	}
	return nil
}
