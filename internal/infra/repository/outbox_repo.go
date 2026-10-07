package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/events"
)

type OutboxItem struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	AggregateID string
	EventType   string
	Payload     []byte
	Status      string
	RetryCount  int
	NextRetryAt time.Time
	CreatedAt   time.Time
}

type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

// Insert saves an event envelope into the transactional outbox table.
func (r *OutboxRepository) Insert(ctx context.Context, tx pgx.Tx, envelope *events.Envelope) error {
	payloadBytes, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal outbox envelope: %w", err)
	}

	query := `
		INSERT INTO outbox (
			id, event_id, aggregate_id, event_type, payload, status,
			retry_count, next_retry_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, 'PENDING', 0, NOW(), NOW())
	`
	_, err = tx.Exec(ctx, query,
		uuid.New(),
		envelope.EventID,
		envelope.AggregateID,
		envelope.EventType,
		payloadBytes,
	)
	if err != nil {
		return fmt.Errorf("failed to insert outbox record: %w", err)
	}
	return nil
}

// ClaimPending finds pending outbox items for publishing using SELECT ... FOR UPDATE SKIP LOCKED.
// This allows multiple concurrent publishers across independent processes to work simultaneously
// without deadlock or duplicate processing.
func (r *OutboxRepository) ClaimPending(ctx context.Context, batchSize int) ([]OutboxItem, error) {
	query := `
		SELECT id, event_id, aggregate_id, event_type, payload, status, retry_count, next_retry_at, created_at
		FROM outbox
		WHERE status = 'PENDING' AND next_retry_at <= NOW()
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, query, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to claim outbox items: %w", err)
	}
	defer rows.Close()

	var items []OutboxItem
	for rows.Next() {
		var item OutboxItem
		if err := rows.Scan(
			&item.ID, &item.EventID, &item.AggregateID, &item.EventType,
			&item.Payload, &item.Status, &item.RetryCount, &item.NextRetryAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return items, nil
}

// MarkPublished marks an outbox event as published.
func (r *OutboxRepository) MarkPublished(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE outbox
		SET status = 'PUBLISHED', published_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// RecordFailure records a publication failure and schedules exponential backoff.
func (r *OutboxRepository) RecordFailure(ctx context.Context, id uuid.UUID, nextRetry time.Time) error {
	query := `
		UPDATE outbox
		SET retry_count = retry_count + 1, next_retry_at = $1
		WHERE id = $2
	`
	_, err := r.pool.Exec(ctx, query, nextRetry, id)
	return err
}
