package usecase

import (
	"context"
	"errors"
	"fmt"

	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/domain/canonical"
	"github.com/junglegaming/backend-challenge-go/internal/domain/events"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
)

var (
	ErrPlayerMismatch   = errors.New("wallet does not belong to specified player")
	ErrReversalMismatch = errors.New("reversal does not match referenced transaction provider, player, wallet, currency or round")
)

type WagerUseCase struct {
	pool       *pgxpool.Pool
	walletRepo *repository.WalletRepository
	ledgerRepo *repository.LedgerRepository
	txRepo     *repository.TransactionRepository
	outboxRepo *repository.OutboxRepository
}

func NewWagerUseCase(
	pool *pgxpool.Pool,
	walletRepo *repository.WalletRepository,
	ledgerRepo *repository.LedgerRepository,
	txRepo *repository.TransactionRepository,
	outboxRepo *repository.OutboxRepository,
) *WagerUseCase {
	return &WagerUseCase{
		pool:       pool,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		txRepo:     txRepo,
		outboxRepo: outboxRepo,
	}
}

type ProcessWagerInput struct {
	ProviderID                     string      `json:"providerId"`
	ExternalTransactionID          string      `json:"externalTransactionId"`
	IdempotencyKey                 string      `json:"idempotencyKey"`
	PlayerID                       uuid.UUID   `json:"playerId"`
	WalletID                       uuid.UUID   `json:"walletId"`
	RoundID                        string      `json:"roundId"`
	GameID                         string      `json:"gameId"`
	Kind                           wager.Kind  `json:"kind"`
	Money                          money.Money `json:"money"`
	ReferenceExternalTransactionID *string     `json:"referenceExternalTransactionId,omitempty"`
}

type ProcessWagerOutput struct {
	TransactionID    uuid.UUID          `json:"transactionId"`
	Status           wager.Status       `json:"status"`
	Balance          *money.Money       `json:"balance,omitempty"`
	FailureCode      *wager.FailureCode `json:"failureCode,omitempty"`
	IdempotentReplay bool               `json:"idempotentReplay"`
}

// Execute processes a wager operation idempotently and atomically with strict financial guarantees.
func (uc *WagerUseCase) Execute(ctx context.Context, input ProcessWagerInput) (*ProcessWagerOutput, error) {
	// 1. Calculate deterministic canonical business payload hash
	var refStr *string
	if input.ReferenceExternalTransactionID != nil && *input.ReferenceExternalTransactionID != "" {
		refStr = input.ReferenceExternalTransactionID
	}

	payloadHash, err := canonical.ComputeBusinessPayloadHash(
		input.ProviderID,
		input.ExternalTransactionID,
		input.PlayerID.String(),
		input.WalletID.String(),
		input.RoundID,
		input.GameID,
		string(input.Kind),
		input.Money.AmountString(),
		input.Money.Currency(),
		refStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute canonical hash: %w", err)
	}

	// 2. Persistent Idempotency Check by Idempotency-Key
	existingByKey, err := uc.txRepo.FindByIdempotencyKey(ctx, input.IdempotencyKey)
	if err == nil && existingByKey != nil {
		// Key exists: check payload hash
		if existingByKey.PayloadHash() != payloadHash {
			return nil, wager.ErrIdempotencyConflict
		}
		// Identical key & payload: return persisted outcome
		return &ProcessWagerOutput{
			TransactionID:    existingByKey.ID(),
			Status:           existingByKey.Status(),
			Balance:          existingByKey.BalanceSnapshot(),
			FailureCode:      existingByKey.FailureCode(),
			IdempotentReplay: true,
		}, nil
	}
	if err != nil && !errors.Is(err, repository.ErrTransactionNotFound) {
		return nil, fmt.Errorf("failed to query idempotency key: %w", err)
	}

	// 3. Persistent Check by (providerId, externalTransactionId)
	existingByExternal, err := uc.txRepo.FindByProviderAndExternalID(ctx, input.ProviderID, input.ExternalTransactionID)
	if err == nil && existingByExternal != nil {
		if existingByExternal.IdempotencyKey() != input.IdempotencyKey {
			return nil, wager.ErrIdempotencyConflict
		}
		return &ProcessWagerOutput{
			TransactionID:    existingByExternal.ID(),
			Status:           existingByExternal.Status(),
			Balance:          existingByExternal.BalanceSnapshot(),
			FailureCode:      existingByExternal.FailureCode(),
			IdempotentReplay: true,
		}, nil
	}
	if err != nil && !errors.Is(err, repository.ErrTransactionNotFound) {
		return nil, fmt.Errorf("failed to query external transaction id: %w", err)
	}

	// 4. Instantiate domain transaction entity
	domainTx, err := wager.NewExternalTransaction(
		input.ProviderID,
		input.ExternalTransactionID,
		input.IdempotencyKey,
		payloadHash,
		input.WalletID,
		input.PlayerID,
		input.RoundID,
		input.GameID,
		input.Kind,
		input.Money,
		refStr,
	)
	if err != nil {
		return nil, err
	}

	// 5. Begin atomic SQL Transaction
	sqlTx, err := uc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin sql tx: %w", err)
	}
	defer sqlTx.Rollback(ctx)

	// 6. Pessimistic Row-Level Lock on Wallet (SELECT FOR UPDATE)
	w, err := uc.walletRepo.FindByIDForUpdate(ctx, sqlTx, input.WalletID)
	if err != nil {
		return nil, err
	}

	// 6.1. Re-check Idempotency inside the serialized lock to catch concurrent identical requests
	existingByKeyTx, err := uc.txRepo.FindByIdempotencyKeyTx(ctx, sqlTx, input.IdempotencyKey)
	if err == nil && existingByKeyTx != nil {
		_ = sqlTx.Rollback(ctx)
		if existingByKeyTx.PayloadHash() != payloadHash {
			return nil, wager.ErrIdempotencyConflict
		}
		return &ProcessWagerOutput{
			TransactionID:    existingByKeyTx.ID(),
			Status:           existingByKeyTx.Status(),
			Balance:          existingByKeyTx.BalanceSnapshot(),
			FailureCode:      existingByKeyTx.FailureCode(),
			IdempotentReplay: true,
		}, nil
	}

	existingByExtTx, err := uc.txRepo.FindByProviderAndExternalIDTx(ctx, sqlTx, input.ProviderID, input.ExternalTransactionID)
	if err == nil && existingByExtTx != nil {
		_ = sqlTx.Rollback(ctx)
		if existingByExtTx.IdempotencyKey() != input.IdempotencyKey {
			return nil, wager.ErrIdempotencyConflict
		}
		return &ProcessWagerOutput{
			TransactionID:    existingByExtTx.ID(),
			Status:           existingByExtTx.Status(),
			Balance:          existingByExtTx.BalanceSnapshot(),
			FailureCode:      existingByExtTx.FailureCode(),
			IdempotentReplay: true,
		}, nil
	}

	if w.PlayerID() != input.PlayerID {
		return nil, ErrPlayerMismatch
	}
	if w.Currency() != input.Money.Currency() {
		return nil, money.ErrCurrencyMismatch
	}

	// 7. Process based on transaction kind
	switch input.Kind {
	case wager.KindBet:
		err = uc.processBet(ctx, sqlTx, w, domainTx)
	case wager.KindWin:
		err = uc.processWin(ctx, sqlTx, w, domainTx)
	case wager.KindLoss:
		err = uc.processLoss(ctx, sqlTx, w, domainTx)
	case wager.KindRefund:
		err = uc.processRefund(ctx, sqlTx, w, domainTx)
	case wager.KindRollback:
		err = uc.processRollback(ctx, sqlTx, w, domainTx)
	default:
		return nil, wager.ErrInvalidKind
	}
	if err != nil {
		if isUniqueViolation(err) {
			_ = sqlTx.Rollback(ctx)
			if existing, qErr := uc.txRepo.FindByIdempotencyKey(ctx, input.IdempotencyKey); qErr == nil && existing != nil {
				if existing.PayloadHash() != payloadHash {
					return nil, wager.ErrIdempotencyConflict
				}
				return &ProcessWagerOutput{
					TransactionID:    existing.ID(),
					Status:           existing.Status(),
					Balance:          existing.BalanceSnapshot(),
					FailureCode:      existing.FailureCode(),
					IdempotentReplay: true,
				}, nil
			}
		}
		return nil, err
	}

	// 8. Commit atomic transaction
	if err := sqlTx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			if existing, qErr := uc.txRepo.FindByIdempotencyKey(ctx, input.IdempotencyKey); qErr == nil && existing != nil {
				if existing.PayloadHash() != payloadHash {
					return nil, wager.ErrIdempotencyConflict
				}
				return &ProcessWagerOutput{
					TransactionID:    existing.ID(),
					Status:           existing.Status(),
					Balance:          existing.BalanceSnapshot(),
					FailureCode:      existing.FailureCode(),
					IdempotentReplay: true,
				}, nil
			}
		}
		return nil, fmt.Errorf("failed to commit wager transaction: %w", err)
	}

	return &ProcessWagerOutput{
		TransactionID:    domainTx.ID(),
		Status:           domainTx.Status(),
		Balance:          domainTx.BalanceSnapshot(),
		FailureCode:      domainTx.FailureCode(),
		IdempotentReplay: false,
	}, nil
}

func (uc *WagerUseCase) processBet(ctx context.Context, tx pgx.Tx, w *wallet.Wallet, dTx *wager.Transaction) error {
	beforeBal, afterBal, err := w.Debit(dTx.Money())
	if err != nil {
		if errors.Is(err, wallet.ErrInsufficientBalance) {
			// Business rejection: Insufficient funds
			_ = dTx.MarkRejected(wager.FailureCodeInsufficientFunds)
			if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
				return err
			}
			return uc.emitRejectedEvent(ctx, tx, dTx, "insufficient funds for bet")
		}
		return err
	}

	// Update locked wallet balance
	if err := uc.walletRepo.UpdateBalance(ctx, tx, w); err != nil {
		return err
	}

	// Mark transaction PROCESSED
	_ = dTx.MarkProcessed(afterBal)
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	// Append immutable ledger entry
	ledgerEntry, err := wallet.NewLedgerEntry(
		w.ID(), dTx.ID(), wallet.DirectionDebit, dTx.Money(), beforeBal, afterBal,
	)
	if err != nil {
		return err
	}
	if err := uc.ledgerRepo.Insert(ctx, tx, ledgerEntry); err != nil {
		return err
	}

	// Outbox events
	return uc.emitProcessedEvents(ctx, tx, w, dTx, wallet.DirectionDebit, beforeBal, afterBal)
}

func (uc *WagerUseCase) processWin(ctx context.Context, tx pgx.Tx, w *wallet.Wallet, dTx *wager.Transaction) error {
	beforeBal, afterBal, err := w.Credit(dTx.Money())
	if err != nil {
		return err
	}

	if err := uc.walletRepo.UpdateBalance(ctx, tx, w); err != nil {
		return err
	}

	_ = dTx.MarkProcessed(afterBal)
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	ledgerEntry, err := wallet.NewLedgerEntry(
		w.ID(), dTx.ID(), wallet.DirectionCredit, dTx.Money(), beforeBal, afterBal,
	)
	if err != nil {
		return err
	}
	if err := uc.ledgerRepo.Insert(ctx, tx, ledgerEntry); err != nil {
		return err
	}

	return uc.emitProcessedEvents(ctx, tx, w, dTx, wallet.DirectionCredit, beforeBal, afterBal)
}

func (uc *WagerUseCase) processLoss(ctx context.Context, tx pgx.Tx, w *wallet.Wallet, dTx *wager.Transaction) error {
	// LOSS does NOT change balance, does NOT increment version, does NOT create ledger
	curBal := w.Balance()
	_ = dTx.MarkProcessed(curBal)
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	// Produces WagerTransactionProcessed, but NOT WalletBalanceChanged
	finBalStr := curBal.String()
	evt, err := events.NewEnvelope(
		events.TypeWagerTransactionProcessed,
		dTx.ID().String(),
		dTx.ID().String(),
		nil,
		1,
		events.WagerTransactionProcessedPayload{
			TransactionID: dTx.ID().String(),
			ProviderID:    dTx.ProviderID(),
			PlayerID:      dTx.PlayerID().String(),
			WalletID:      dTx.WalletID().String(),
			RoundID:       dTx.RoundID(),
			GameID:        dTx.GameID(),
			Kind:          string(dTx.Kind()),
			Money:         dTx.Money(),
			Status:        string(wager.StatusProcessed),
			FinalBalance:  &finBalStr,
		},
	)
	if err != nil {
		return err
	}
	return uc.outboxRepo.Insert(ctx, tx, evt)
}

func (uc *WagerUseCase) processRefund(ctx context.Context, tx pgx.Tx, w *wallet.Wallet, dTx *wager.Transaction) error {
	refExtID := *dTx.ReferenceExternalTransactionID()

	// Find referenced transaction
	refTx, err := uc.txRepo.FindByProviderAndExternalID(ctx, dTx.ProviderID(), refExtID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			// Referenced transaction not yet received: persist as PENDING_REFERENCE
			return uc.deferPendingReference(ctx, tx, dTx)
		}
		return err
	}

	if refTx.Status() != wager.StatusProcessed {
		return uc.deferPendingReference(ctx, tx, dTx)
	}

	// Validate matching attributes
	if refTx.Kind() != wager.KindBet ||
		refTx.PlayerID() != dTx.PlayerID() ||
		refTx.WalletID() != dTx.WalletID() ||
		refTx.RoundID() != dTx.RoundID() ||
		!refTx.Money().Equals(dTx.Money()) {
		_ = dTx.MarkRejected(wager.FailureCodeInvalidReversalMatch)
		if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
			return err
		}
		return uc.emitRejectedEvent(ctx, tx, dTx, "refund attributes do not match referenced bet")
	}

	// Check double reversal (REFUND or ROLLBACK)
	alreadyReversed, err := uc.txRepo.HasExistingReversalTx(ctx, tx, dTx.ProviderID(), refExtID)
	if err != nil {
		return err
	}
	if alreadyReversed {
		_ = dTx.MarkRejected(wager.FailureCodeDoubleReversal)
		if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
			return err
		}
		return uc.emitRejectedEvent(ctx, tx, dTx, "bet already reversed by an existing refund or rollback")
	}

	// Credit wallet for refund
	beforeBal, afterBal, err := w.Credit(dTx.Money())
	if err != nil {
		return err
	}

	if err := uc.walletRepo.UpdateBalance(ctx, tx, w); err != nil {
		return err
	}

	dTx.SetResolvedReference(refTx.ID())
	_ = dTx.MarkProcessed(afterBal)
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	ledgerEntry, err := wallet.NewLedgerEntry(
		w.ID(), dTx.ID(), wallet.DirectionCredit, dTx.Money(), beforeBal, afterBal,
	)
	if err != nil {
		return err
	}
	if err := uc.ledgerRepo.Insert(ctx, tx, ledgerEntry); err != nil {
		return err
	}

	return uc.emitProcessedEvents(ctx, tx, w, dTx, wallet.DirectionCredit, beforeBal, afterBal)
}

func (uc *WagerUseCase) processRollback(ctx context.Context, tx pgx.Tx, w *wallet.Wallet, dTx *wager.Transaction) error {
	refExtID := *dTx.ReferenceExternalTransactionID()

	refTx, err := uc.txRepo.FindByProviderAndExternalID(ctx, dTx.ProviderID(), refExtID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return uc.deferPendingReference(ctx, tx, dTx)
		}
		return err
	}

	if refTx.Status() != wager.StatusProcessed {
		return uc.deferPendingReference(ctx, tx, dTx)
	}

	if refTx.PlayerID() != dTx.PlayerID() ||
		refTx.WalletID() != dTx.WalletID() ||
		refTx.RoundID() != dTx.RoundID() ||
		!refTx.Money().Equals(dTx.Money()) {
		_ = dTx.MarkRejected(wager.FailureCodeInvalidReversalMatch)
		if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
			return err
		}
		return uc.emitRejectedEvent(ctx, tx, dTx, "rollback attributes do not match referenced transaction")
	}

	// Check double reversal (REFUND or ROLLBACK)
	alreadyReversed, err := uc.txRepo.HasExistingReversalTx(ctx, tx, dTx.ProviderID(), refExtID)
	if err != nil {
		return err
	}
	if alreadyReversed {
		_ = dTx.MarkRejected(wager.FailureCodeDoubleReversal)
		if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
			return err
		}
		return uc.emitRejectedEvent(ctx, tx, dTx, "transaction already reversed by an existing refund or rollback")
	}

	// Counter movement based on referenced kind:
	// BET was a DEBIT -> counter movement is CREDIT
	// WIN was a CREDIT -> counter movement is DEBIT
	// REFUND was a CREDIT -> counter movement is DEBIT
	var (
		beforeBal, afterBal money.Money
		dir                 wallet.Direction
	)

	switch refTx.Kind() {
	case wager.KindBet:
		dir = wallet.DirectionCredit
		beforeBal, afterBal, err = w.Credit(dTx.Money())
		if err != nil {
			return err
		}
	case wager.KindWin, wager.KindRefund:
		dir = wallet.DirectionDebit
		beforeBal, afterBal, err = w.Debit(dTx.Money())
		if err != nil {
			if errors.Is(err, wallet.ErrInsufficientBalance) {
				_ = dTx.MarkRejected(wager.FailureCodeInsufficientFunds)
				if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
					return err
				}
				return uc.emitRejectedEvent(ctx, tx, dTx, "insufficient funds to rollback win/refund")
			}
			return err
		}
	default:
		return wager.ErrInvalidKind
	}

	if err := uc.walletRepo.UpdateBalance(ctx, tx, w); err != nil {
		return err
	}

	dTx.SetResolvedReference(refTx.ID())
	_ = dTx.MarkProcessed(afterBal)
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	ledgerEntry, err := wallet.NewLedgerEntry(
		w.ID(), dTx.ID(), dir, dTx.Money(), beforeBal, afterBal,
	)
	if err != nil {
		return err
	}
	if err := uc.ledgerRepo.Insert(ctx, tx, ledgerEntry); err != nil {
		return err
	}

	return uc.emitProcessedEvents(ctx, tx, w, dTx, dir, beforeBal, afterBal)
}

func (uc *WagerUseCase) deferPendingReference(ctx context.Context, tx pgx.Tx, dTx *wager.Transaction) error {
	_ = dTx.MarkPendingReference()
	if err := uc.txRepo.Create(ctx, tx, dTx); err != nil {
		return err
	}

	refExtID := ""
	if dTx.ReferenceExternalTransactionID() != nil {
		refExtID = *dTx.ReferenceExternalTransactionID()
	}

	evt, err := events.NewEnvelope(
		events.TypeWagerTransactionPendingReference,
		dTx.ID().String(),
		dTx.ID().String(),
		nil,
		1,
		events.WagerTransactionPendingReferencePayload{
			TransactionID:                  dTx.ID().String(),
			ProviderID:                     dTx.ProviderID(),
			ReferenceExternalTransactionID: refExtID,
			Kind:                           string(dTx.Kind()),
			Money:                          dTx.Money(),
		},
	)
	if err != nil {
		return err
	}
	return uc.outboxRepo.Insert(ctx, tx, evt)
}

func (uc *WagerUseCase) emitProcessedEvents(
	ctx context.Context,
	tx pgx.Tx,
	w *wallet.Wallet,
	dTx *wager.Transaction,
	dir wallet.Direction,
	beforeBal, afterBal money.Money,
) error {
	finBalStr := afterBal.String()
	procEvt, err := events.NewEnvelope(
		events.TypeWagerTransactionProcessed,
		dTx.ID().String(),
		dTx.ID().String(),
		nil,
		1,
		events.WagerTransactionProcessedPayload{
			TransactionID: dTx.ID().String(),
			ProviderID:    dTx.ProviderID(),
			PlayerID:      dTx.PlayerID().String(),
			WalletID:      dTx.WalletID().String(),
			RoundID:       dTx.RoundID(),
			GameID:        dTx.GameID(),
			Kind:          string(dTx.Kind()),
			Money:         dTx.Money(),
			Status:        string(wager.StatusProcessed),
			FinalBalance:  &finBalStr,
		},
	)
	if err != nil {
		return err
	}
	if err := uc.outboxRepo.Insert(ctx, tx, procEvt); err != nil {
		return err
	}

	balEvt, err := events.NewEnvelope(
		events.TypeWalletBalanceChanged,
		w.ID().String(),
		dTx.ID().String(),
		nil,
		1,
		events.WalletBalanceChangedPayload{
			WalletID:      w.ID().String(),
			TransactionID: dTx.ID().String(),
			Direction:     string(dir),
			Money:         dTx.Money(),
			BalanceBefore: beforeBal.String(),
			BalanceAfter:  afterBal.String(),
			WalletVersion: w.Version(),
		},
	)
	if err != nil {
		return err
	}
	return uc.outboxRepo.Insert(ctx, tx, balEvt)
}

func (uc *WagerUseCase) emitRejectedEvent(ctx context.Context, tx pgx.Tx, dTx *wager.Transaction, reason string) error {
	codeStr := ""
	if dTx.FailureCode() != nil {
		codeStr = string(*dTx.FailureCode())
	}

	rejEvt, err := events.NewEnvelope(
		events.TypeWagerTransactionRejected,
		dTx.ID().String(),
		dTx.ID().String(),
		nil,
		1,
		events.WagerTransactionRejectedPayload{
			TransactionID: dTx.ID().String(),
			ProviderID:    dTx.ProviderID(),
			FailureCode:   codeStr,
			Reason:        reason,
			Kind:          string(dTx.Kind()),
			Money:         dTx.Money(),
		},
	)
	if err != nil {
		return err
	}
	return uc.outboxRepo.Insert(ctx, tx, rejEvt)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}
