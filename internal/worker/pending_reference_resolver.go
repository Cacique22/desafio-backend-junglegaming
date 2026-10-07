package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/events"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
)

type PendingReferenceResolver struct {
	logger     *slog.Logger
	pool       *pgxpool.Pool
	txRepo     *repository.TransactionRepository
	walletRepo *repository.WalletRepository
	ledgerRepo *repository.LedgerRepository
	outboxRepo *repository.OutboxRepository
	ttl        time.Duration
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewPendingReferenceResolver(
	logger *slog.Logger,
	pool *pgxpool.Pool,
	txRepo *repository.TransactionRepository,
	walletRepo *repository.WalletRepository,
	ledgerRepo *repository.LedgerRepository,
	outboxRepo *repository.OutboxRepository,
) *PendingReferenceResolver {
	return &PendingReferenceResolver{
		logger:     logger,
		pool:       pool,
		txRepo:     txRepo,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		outboxRepo: outboxRepo,
		ttl:        24 * time.Hour,
		stopCh:     make(chan struct{}),
	}
}

func (r *PendingReferenceResolver) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.run(ctx)
}

func (r *PendingReferenceResolver) Stop(ctx context.Context) error {
	close(r.stopCh)
	c := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		r.logger.Info("pending reference resolver stopped gracefully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *PendingReferenceResolver) run(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.resolveBatch(ctx)
		}
	}
}

func (r *PendingReferenceResolver) resolveBatch(ctx context.Context) {
	pending, err := r.txRepo.FindPendingReferences(ctx, 20)
	if err != nil {
		r.logger.Error("failed to query pending references", "error", err)
		return
	}

	for _, pTx := range pending {
		r.processSinglePending(ctx, pTx)
	}
}

func (r *PendingReferenceResolver) processSinglePending(ctx context.Context, pTx *wager.Transaction) {
	// Check expiration
	if time.Since(pTx.CreatedAt()) > r.ttl {
		r.expirePending(ctx, pTx)
		return
	}

	if pTx.ReferenceExternalTransactionID() == nil {
		return
	}
	refExtID := *pTx.ReferenceExternalTransactionID()

	// Look up reference
	refTx, err := r.txRepo.FindByProviderAndExternalID(ctx, pTx.ProviderID(), refExtID)
	if err != nil || refTx == nil || refTx.Status() != wager.StatusProcessed {
		// Still unavailable
		return
	}

	// Reference now exists and is PROCESSED! Execute reversal
	sqlTx, err := r.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer sqlTx.Rollback(ctx)

	w, err := r.walletRepo.FindByIDForUpdate(ctx, sqlTx, pTx.WalletID())
	if err != nil {
		return
	}

	var (
		beforeBal, afterBal money.Money
		dir                 wallet.Direction
	)

	switch pTx.Kind() {
	case wager.KindRefund:
		dir = wallet.DirectionCredit
		beforeBal, afterBal, err = w.Credit(pTx.Money())
	case wager.KindRollback:
		if refTx.Kind() == wager.KindBet {
			dir = wallet.DirectionCredit
			beforeBal, afterBal, err = w.Credit(pTx.Money())
		} else {
			dir = wallet.DirectionDebit
			beforeBal, afterBal, err = w.Debit(pTx.Money())
		}
	default:
		return
	}

	if err != nil {
		_ = pTx.MarkRejected(wager.FailureCodeInsufficientFunds)
		_ = r.txRepo.Update(ctx, sqlTx, pTx)
		_ = sqlTx.Commit(ctx)
		return
	}

	if err := r.walletRepo.UpdateBalance(ctx, sqlTx, w); err != nil {
		return
	}

	ledgerEntry, err := wallet.NewLedgerEntry(
		w.ID(), pTx.ID(), dir, pTx.Money(), beforeBal, afterBal,
	)
	if err != nil {
		return
	}
	if err := r.ledgerRepo.Insert(ctx, sqlTx, ledgerEntry); err != nil {
		return
	}

	pTx.SetResolvedReference(refTx.ID())
	_ = pTx.MarkProcessed(afterBal)
	if err := r.txRepo.Update(ctx, sqlTx, pTx); err != nil {
		return
	}

	// Outbox
	finBalStr := afterBal.String()
	procEvt, _ := events.NewEnvelope(
		events.TypeWagerTransactionProcessed,
		pTx.ID().String(),
		pTx.ID().String(),
		nil,
		1,
		events.WagerTransactionProcessedPayload{
			TransactionID: pTx.ID().String(),
			ProviderID:    pTx.ProviderID(),
			PlayerID:      pTx.PlayerID().String(),
			WalletID:      pTx.WalletID().String(),
			RoundID:       pTx.RoundID(),
			GameID:        pTx.GameID(),
			Kind:          string(pTx.Kind()),
			Money:         pTx.Money(),
			Status:        string(wager.StatusProcessed),
			FinalBalance:  &finBalStr,
		},
	)
	_ = r.outboxRepo.Insert(ctx, sqlTx, procEvt)

	_ = sqlTx.Commit(ctx)
	r.logger.Info("resolved pending reference transaction", "txId", pTx.ID())
}

func (r *PendingReferenceResolver) expirePending(ctx context.Context, pTx *wager.Transaction) {
	sqlTx, err := r.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer sqlTx.Rollback(ctx)

	_ = pTx.MarkRejected(wager.FailureCodeReferenceExpired)
	_ = r.txRepo.Update(ctx, sqlTx, pTx)

	rejEvt, _ := events.NewEnvelope(
		events.TypeWagerTransactionRejected,
		pTx.ID().String(),
		pTx.ID().String(),
		nil,
		1,
		events.WagerTransactionRejectedPayload{
			TransactionID: pTx.ID().String(),
			ProviderID:    pTx.ProviderID(),
			FailureCode:   string(wager.FailureCodeReferenceExpired),
			Reason:        "pending reference expired past TTL",
			Kind:          string(pTx.Kind()),
			Money:         pTx.Money(),
		},
	)
	_ = r.outboxRepo.Insert(ctx, sqlTx, rejEvt)
	_ = sqlTx.Commit(ctx)
	r.logger.Warn("expired pending reference transaction", "txId", pTx.ID())
}
