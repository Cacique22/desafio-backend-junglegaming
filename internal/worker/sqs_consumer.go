package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
	"github.com/junglegaming/backend-challenge-go/internal/usecase"
)

type SQSConsumer struct {
	logger       *slog.Logger
	pool         *pgxpool.Pool
	inboxRepo    *repository.InboxRepository
	wagerUseCase *usecase.WagerUseCase
	sqsClient    *sqs.Client
	queueURL     string
	consumerName string
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

func NewSQSConsumer(
	logger *slog.Logger,
	pool *pgxpool.Pool,
	inboxRepo *repository.InboxRepository,
	wagerUseCase *usecase.WagerUseCase,
	sqsClient *sqs.Client,
	queueURL string,
) *SQSConsumer {
	return &SQSConsumer{
		logger:       logger,
		pool:         pool,
		inboxRepo:    inboxRepo,
		wagerUseCase: wagerUseCase,
		sqsClient:    sqsClient,
		queueURL:     queueURL,
		consumerName: "wager-sqs-consumer",
		stopCh:       make(chan struct{}),
	}
}

type SQSMessageEnvelope struct {
	MessageID  string          `json:"messageId"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurredAt"`
	Data       WagerMessageData `json:"data"`
}

type WagerMessageData struct {
	ProviderID                      string      `json:"providerId"`
	ExternalTransactionID          string      `json:"externalTransactionId"`
	IdempotencyKey                  string      `json:"idempotencyKey"`
	PlayerID                        string      `json:"playerId"`
	WalletID                        string      `json:"walletId"`
	RoundID                         string      `json:"roundId"`
	GameID                          string      `json:"gameId"`
	Kind                            string      `json:"kind"`
	Money                           money.Money `json:"money"`
	ReferenceExternalTransactionID *string     `json:"referenceExternalTransactionId,omitempty"`
}

func (c *SQSConsumer) Start(ctx context.Context) {
	if c.sqsClient == nil || c.queueURL == "" {
		c.logger.Info("sqs consumer disabled (no client or queueURL configured)")
		return
	}
	c.wg.Add(1)
	go c.run(ctx)
}

func (c *SQSConsumer) Stop(ctx context.Context) error {
	close(c.stopCh)
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("sqs consumer stopped gracefully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *SQSConsumer) run(ctx context.Context) {
	defer c.wg.Done()
	c.logger.Info("starting sqs consumer loop", "queueUrl", c.queueURL)

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		default:
			c.fetchAndProcess(ctx)
		}
	}
}

func (c *SQSConsumer) fetchAndProcess(ctx context.Context) {
	output, err := c.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &c.queueURL,
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     10, // Long polling
		VisibilityTimeout:   30,
	})
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.logger.Error("error receiving messages from sqs", "error", err)
			time.Sleep(1 * time.Second)
		}
		return
	}

	for _, msg := range output.Messages {
		c.processMessage(ctx, msg)
	}
}

func (c *SQSConsumer) processMessage(ctx context.Context, msg sqsTypes.Message) {
	if msg.Body == nil {
		return
	}

	var env SQSMessageEnvelope
	if err := json.Unmarshal([]byte(*msg.Body), &env); err != nil {
		c.logger.Error("malformed message body in sqs", "error", err)
		return
	}

	playerUUID, err := uuid.Parse(env.Data.PlayerID)
	if err != nil {
		c.logger.Error("invalid player UUID in sqs message", "error", err)
		return
	}

	walletUUID, err := uuid.Parse(env.Data.WalletID)
	if err != nil {
		c.logger.Error("invalid wallet UUID in sqs message", "error", err)
		return
	}

	hash := sha256.Sum256([]byte(*msg.Body))
	payloadHash := hex.EncodeToString(hash[:])

	// Atomic inbox deduplication check
	sqlTx, err := c.pool.Begin(ctx)
	if err != nil {
		c.logger.Error("failed to start transaction for inbox check", "error", err)
		return
	}
	defer sqlTx.Rollback(ctx)

	err = c.inboxRepo.RecordOrCheck(ctx, sqlTx, c.consumerName, env.MessageID, payloadHash)
	if err != nil {
		if errors.Is(err, repository.ErrInboxDuplicateCompleted) {
			c.logger.Info("inbox message already completed, deleting from sqs", "messageId", env.MessageID)
			_ = c.deleteMessage(ctx, msg.ReceiptHandle)
			return
		}
		c.logger.Error("inbox check failed", "error", err)
		return
	}

	// Commit inbox reservation
	if err := sqlTx.Commit(ctx); err != nil {
		c.logger.Error("failed to commit inbox check", "error", err)
		return
	}

	// Execute business wager logic
	_, err = c.wagerUseCase.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                      env.Data.ProviderID,
		ExternalTransactionID:          env.Data.ExternalTransactionID,
		IdempotencyKey:                  env.Data.IdempotencyKey,
		PlayerID:                        playerUUID,
		WalletID:                        walletUUID,
		RoundID:                         env.Data.RoundID,
		GameID:                          env.Data.GameID,
		Kind:                            wager.Kind(env.Data.Kind),
		Money:                           env.Data.Money,
		ReferenceExternalTransactionID: env.Data.ReferenceExternalTransactionID,
	})
	if err != nil {
		c.logger.Error("failed to execute wager use case from sqs", "error", err)
		return
	}

	// Mark inbox completed
	completeTx, err := c.pool.Begin(ctx)
	if err == nil {
		_ = c.inboxRepo.MarkCompleted(ctx, completeTx, c.consumerName, env.MessageID)
		_ = completeTx.Commit(ctx)
	}

	// CRITICAL: Delete from SQS only after durable DB commit
	_ = c.deleteMessage(ctx, msg.ReceiptHandle)
}

func (c *SQSConsumer) deleteMessage(ctx context.Context, receiptHandle *string) error {
	if receiptHandle == nil {
		return nil
	}
	_, err := c.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &c.queueURL,
		ReceiptHandle: receiptHandle,
	})
	return err
}
