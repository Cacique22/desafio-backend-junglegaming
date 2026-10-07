package wager

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

var (
	ErrInvalidKind                  = errors.New("invalid transaction kind")
	ErrInvalidStatusTransition      = errors.New("invalid status transition")
	ErrTerminalStateImmutable       = errors.New("terminal state is immutable and cannot be transitioned")
	ErrOpeningNotAllowedExternal    = errors.New("OPENING is an internal operation and cannot be submitted externally")
	ErrZeroAmountNotAllowedForKind  = errors.New("amount must be greater than zero for this transaction kind")
	ErrLossMustBeZero               = errors.New("LOSS must have amount equal to 0.00")
	ErrMissingReferenceForReversal  = errors.New("referenceExternalTransactionId is required for REFUND and ROLLBACK")
	ErrIdempotencyConflict          = errors.New("idempotency key reused with conflicting payload")
	ErrDoubleReversal               = errors.New("transaction has already been reversed by this reversal type")
	ErrReferenceMismatch            = errors.New("reversal does not match referenced transaction provider, player, wallet or round")
)

type Kind string

const (
	KindOpening  Kind = "OPENING"
	KindBet      Kind = "BET"
	KindWin      Kind = "WIN"
	KindLoss     Kind = "LOSS"
	KindRefund   Kind = "REFUND"
	KindRollback Kind = "ROLLBACK"
)

type Status string

const (
	StatusPending          Status = "PENDING"
	StatusPendingReference Status = "PENDING_REFERENCE"
	StatusProcessed        Status = "PROCESSED"
	StatusRejected         Status = "REJECTED"
	StatusFailed           Status = "FAILED"
)

func (s Status) IsTerminal() bool {
	return s == StatusProcessed || s == StatusRejected || s == StatusFailed
}

type FailureCode string

const (
	FailureCodeInsufficientFunds     FailureCode = "INSUFFICIENT_FUNDS"
	FailureCodeReferenceNotFound     FailureCode = "REFERENCE_NOT_FOUND"
	FailureCodeReferenceExpired      FailureCode = "REFERENCE_EXPIRED"
	FailureCodeDoubleReversal        FailureCode = "DOUBLE_REVERSAL"
	FailureCodeInvalidReversalMatch  FailureCode = "INVALID_REVERSAL_MATCH"
	FailureCodeInternalError         FailureCode = "INTERNAL_ERROR"
	FailureCodeCurrencyMismatch      FailureCode = "CURRENCY_MISMATCH"
)

// Transaction represents a financial wagering operation.
type Transaction struct {
	id                              uuid.UUID
	providerID                      string
	externalTransactionID          string
	idempotencyKey                  string
	payloadHash                     string
	walletID                        uuid.UUID
	playerID                        uuid.UUID
	roundID                         string
	gameID                          string
	kind                            Kind
	money                           money.Money
	referenceExternalTransactionID *string
	resolvedReferenceID             *uuid.UUID
	status                          Status
	failureCode                     *FailureCode
	balanceSnapshot                 *money.Money
	createdAt                       time.Time
	updatedAt                       time.Time
}

// NewExternalTransaction creates an external wager transaction initiated via HTTP or SQS.
func NewExternalTransaction(
	providerID string,
	externalTransactionID string,
	idempotencyKey string,
	payloadHash string,
	walletID uuid.UUID,
	playerID uuid.UUID,
	roundID string,
	gameID string,
	kind Kind,
	m money.Money,
	refExternalID *string,
) (*Transaction, error) {
	if kind == KindOpening {
		return nil, ErrOpeningNotAllowedExternal
	}

	switch kind {
	case KindBet, KindWin:
		if !m.IsPositive() {
			return nil, ErrZeroAmountNotAllowedForKind
		}
	case KindLoss:
		if !m.IsZero() {
			return nil, ErrLossMustBeZero
		}
	case KindRefund, KindRollback:
		if !m.IsPositive() {
			return nil, ErrZeroAmountNotAllowedForKind
		}
		if refExternalID == nil || *refExternalID == "" {
			return nil, ErrMissingReferenceForReversal
		}
	default:
		return nil, ErrInvalidKind
	}

	now := time.Now().UTC()
	return &Transaction{
		id:                              uuid.New(),
		providerID:                      providerID,
		externalTransactionID:          externalTransactionID,
		idempotencyKey:                  idempotencyKey,
		payloadHash:                     payloadHash,
		walletID:                        walletID,
		playerID:                        playerID,
		roundID:                         roundID,
		gameID:                          gameID,
		kind:                            kind,
		money:                           m,
		referenceExternalTransactionID: refExternalID,
		status:                          StatusPending,
		createdAt:                       now,
		updatedAt:                       now,
	}, nil
}

// NewInternalOpening creates an internal wallet opening transaction.
func NewInternalOpening(
	walletID uuid.UUID,
	playerID uuid.UUID,
	m money.Money,
) (*Transaction, error) {
	now := time.Now().UTC()
	snapshot := m
	return &Transaction{
		id:               uuid.New(),
		providerID:       "SYSTEM",
		externalTransactionID: "opening-" + walletID.String(),
		idempotencyKey:   "opening-" + walletID.String(),
		payloadHash:      "INTERNAL_OPENING",
		walletID:         walletID,
		playerID:         playerID,
		roundID:          "OPENING",
		gameID:           "SYSTEM",
		kind:             KindOpening,
		money:            m,
		status:           StatusProcessed,
		balanceSnapshot:  &snapshot,
		createdAt:        now,
		updatedAt:        now,
	}, nil
}

// Rehydrate constructs an existing transaction from the database.
func Rehydrate(
	id uuid.UUID,
	providerID string,
	externalTransactionID string,
	idempotencyKey string,
	payloadHash string,
	walletID uuid.UUID,
	playerID uuid.UUID,
	roundID string,
	gameID string,
	kind Kind,
	m money.Money,
	refExternalID *string,
	resolvedRefID *uuid.UUID,
	status Status,
	failureCode *FailureCode,
	balanceSnapshot *money.Money,
	createdAt time.Time,
	updatedAt time.Time,
) *Transaction {
	return &Transaction{
		id:                              id,
		providerID:                      providerID,
		externalTransactionID:          externalTransactionID,
		idempotencyKey:                  idempotencyKey,
		payloadHash:                     payloadHash,
		walletID:                        walletID,
		playerID:                        playerID,
		roundID:                         roundID,
		gameID:                          gameID,
		kind:                            kind,
		money:                           m,
		referenceExternalTransactionID: refExternalID,
		resolvedReferenceID:             resolvedRefID,
		status:                          status,
		failureCode:                     failureCode,
		balanceSnapshot:                 balanceSnapshot,
		createdAt:                       createdAt,
		updatedAt:                       updatedAt,
	}
}

// MarkProcessed transitions transaction to PROCESSED with resulting balance snapshot.
func (t *Transaction) MarkProcessed(finalBalance money.Money) error {
	if t.status.IsTerminal() {
		return ErrTerminalStateImmutable
	}
	t.status = StatusProcessed
	t.balanceSnapshot = &finalBalance
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkPendingReference transitions transaction to PENDING_REFERENCE when waiting for missing reference.
func (t *Transaction) MarkPendingReference() error {
	if t.status.IsTerminal() {
		return ErrTerminalStateImmutable
	}
	t.status = StatusPendingReference
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkRejected transitions transaction to REJECTED with business failure code.
func (t *Transaction) MarkRejected(code FailureCode) error {
	if t.status.IsTerminal() {
		return ErrTerminalStateImmutable
	}
	t.status = StatusRejected
	t.failureCode = &code
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions transaction to FAILED on permanent unrecoverable failure.
func (t *Transaction) MarkFailed(code FailureCode) error {
	if t.status.IsTerminal() {
		return ErrTerminalStateImmutable
	}
	t.status = StatusFailed
	t.failureCode = &code
	t.updatedAt = time.Now().UTC()
	return nil
}

// SetResolvedReference links the internal ID of the resolved reference transaction.
func (t *Transaction) SetResolvedReference(refID uuid.UUID) {
	t.resolvedReferenceID = &refID
}

func (t *Transaction) ID() uuid.UUID                               { return t.id }
func (t *Transaction) ProviderID() string                          { return t.providerID }
func (t *Transaction) ExternalTransactionID() string               { return t.externalTransactionID }
func (t *Transaction) IdempotencyKey() string                      { return t.idempotencyKey }
func (t *Transaction) PayloadHash() string                         { return t.payloadHash }
func (t *Transaction) WalletID() uuid.UUID                         { return t.walletID }
func (t *Transaction) PlayerID() uuid.UUID                         { return t.playerID }
func (t *Transaction) RoundID() string                             { return t.roundID }
func (t *Kind) String() string                                     { return string(*t) }
func (t *Transaction) Kind() Kind                                  { return t.kind }
func (t *Transaction) GameID() string                              { return t.gameID }
func (t *Transaction) Money() money.Money                          { return t.money }
func (t *Transaction) ReferenceExternalTransactionID() *string     { return t.referenceExternalTransactionID }
func (t *Transaction) ResolvedReferenceID() *uuid.UUID             { return t.resolvedReferenceID }
func (t *Transaction) Status() Status                              { return t.status }
func (t *Transaction) FailureCode() *FailureCode                   { return t.failureCode }
func (t *Transaction) BalanceSnapshot() *money.Money               { return t.balanceSnapshot }
func (t *Transaction) CreatedAt() time.Time                        { return t.createdAt }
func (t *Transaction) UpdatedAt() time.Time                        { return t.updatedAt }
