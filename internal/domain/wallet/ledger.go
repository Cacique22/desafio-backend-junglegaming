package wallet

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

var (
	ErrInvalidLedgerMath = errors.New("ledger entry math is invalid: balanceAfter != balanceBefore ± amount")
	ErrInvalidDirection  = errors.New("direction must be DEBIT or CREDIT")
)

type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

// LedgerEntry represents an immutable, append-only record in the wallet ledger.
// It proves the exact balance before and after each financial transaction.
type LedgerEntry struct {
	id            uuid.UUID
	walletID      uuid.UUID
	transactionID uuid.UUID
	direction     Direction
	amount        money.Money
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

// NewLedgerEntry creates a validated, immutable LedgerEntry.
// Invariant:
// For CREDIT: balanceAfter = balanceBefore + amount
// For DEBIT:  balanceAfter = balanceBefore - amount
func NewLedgerEntry(
	walletID uuid.UUID,
	transactionID uuid.UUID,
	direction Direction,
	amount money.Money,
	balanceBefore money.Money,
	balanceAfter money.Money,
) (*LedgerEntry, error) {
	if walletID == uuid.Nil {
		return nil, ErrInvalidWalletID
	}
	if transactionID == uuid.Nil {
		return nil, errors.New("transaction ID must be a valid non-empty UUID")
	}
	if !amount.IsPositive() {
		return nil, ErrInvalidAmount
	}

	switch direction {
	case DirectionCredit:
		expected, err := balanceBefore.Add(amount)
		if err != nil || !expected.Equals(balanceAfter) {
			return nil, ErrInvalidLedgerMath
		}
	case DirectionDebit:
		expected, err := balanceBefore.Sub(amount)
		if err != nil || !expected.Equals(balanceAfter) {
			return nil, ErrInvalidLedgerMath
		}
	default:
		return nil, ErrInvalidDirection
	}

	return &LedgerEntry{
		id:            uuid.New(),
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     time.Now().UTC(),
	}, nil
}

// RehydrateLedgerEntry rehydrates an existing ledger entry from database.
func RehydrateLedgerEntry(
	id uuid.UUID,
	walletID uuid.UUID,
	transactionID uuid.UUID,
	direction Direction,
	amount money.Money,
	balanceBefore money.Money,
	balanceAfter money.Money,
	createdAt time.Time,
) *LedgerEntry {
	return &LedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt,
	}
}

func (l *LedgerEntry) ID() uuid.UUID            { return l.id }
func (l *LedgerEntry) WalletID() uuid.UUID      { return l.walletID }
func (l *LedgerEntry) TransactionID() uuid.UUID { return l.transactionID }
func (l *LedgerEntry) Direction() Direction     { return l.direction }
func (l *LedgerEntry) Amount() money.Money      { return l.amount }
func (l *LedgerEntry) BalanceBefore() money.Money { return l.balanceBefore }
func (l *LedgerEntry) BalanceAfter() money.Money  { return l.balanceAfter }
func (l *LedgerEntry) CreatedAt() time.Time     { return l.createdAt }
