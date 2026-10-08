package worker

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
)

type OutboxPublisher struct {
	logger     *slog.Logger
	outboxRepo *repository.OutboxRepository
	sqsClient  *sqs.Client
	queueURL   string
	batchSize  int
	pollPeriod time.Duration
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewOutboxPublisher(
	logger *slog.Logger,
	outboxRepo *repository.OutboxRepository,
	sqsClient *sqs.Client,
	queueURL string,
) *OutboxPublisher {
	return &OutboxPublisher{
		logger:     logger,
		outboxRepo: outboxRepo,
		sqsClient:  sqsClient,
		queueURL:   queueURL,
		batchSize:  50,
		pollPeriod: 500 * time.Millisecond,
	}
}

func (p *OutboxPublisher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.run(ctx)
}

func (p *OutboxPublisher) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	c := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		p.logger.Info("outbox publisher stopped gracefully")
		return nil
	case <-ctx.Done():
		p.logger.Warn("outbox publisher forced shutdown on timeout")
		return ctx.Err()
	}
}

func (p *OutboxPublisher) run(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.pollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processBatch(ctx)
		}
	}
}

func (p *OutboxPublisher) processBatch(ctx context.Context) {
	items, err := p.outboxRepo.ClaimPending(ctx, p.batchSize)
	if err != nil {
		p.logger.Error("failed to claim pending outbox events", "error", err)
		return
	}

	for _, item := range items {
		// Attempt publish
		err := p.publishEvent(ctx, item)
		if err != nil {
			p.logger.Error("failed to publish outbox event", "eventId", item.EventID, "error", err)
			backoffSec := math.Pow(2, float64(item.RetryCount+1))
			if backoffSec > 60 {
				backoffSec = 60
			}
			nextRetry := time.Now().UTC().Add(time.Duration(backoffSec) * time.Second)
			_ = p.outboxRepo.RecordFailure(ctx, item.ID, nextRetry)
			continue
		}

		// Mark published
		if err := p.outboxRepo.MarkPublished(ctx, item.ID); err != nil {
			p.logger.Error("failed to mark outbox as published", "id", item.ID, "error", err)
		} else {
			p.logger.Debug("published outbox event", "eventId", item.EventID, "type", item.EventType)
		}
	}
}

func (p *OutboxPublisher) publishEvent(ctx context.Context, item repository.OutboxItem) error {
	if p.sqsClient != nil && p.queueURL != "" {
		body := string(item.Payload)
		msgGroupID := item.AggregateID
		msgDeduplicationID := item.EventID.String()

		_, err := p.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               &p.queueURL,
			MessageBody:            &body,
			MessageGroupId:         &msgGroupID,
			MessageDeduplicationId: &msgDeduplicationID,
		})
		return err
	}
	return nil
}
