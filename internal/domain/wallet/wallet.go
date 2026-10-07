package wallet

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance for debit operation")
	ErrInvalidAmount       = errors.New("operation amount must be strictly greater than zero")
	ErrInvalidPlayerID     = errors.New("player ID must be a valid non-empty UUID")
	ErrInvalidWalletID     = errors.New("wallet ID must be a valid non-empty UUID")
)

// Wallet is the aggregate root for player funds.
// It maintains financial invariants:
// 1. Balance must never be negative.
// 2. Version starts at 1 and only increments when balance changes.
// 3. Currency is immutable.
type Wallet struct {
	id        uuid.UUID
	playerID  uuid.UUID
	currency  string
	balance   money.Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

// New creates a new Wallet aggregate with initial version 1.
func New(playerID uuid.UUID, initialBalance money.Money) (*Wallet, error) {
	if playerID == uuid.Nil {
		return nil, ErrInvalidPlayerID
	}
	if initialBalance.IsNegative() {
		return nil, ErrInsufficientBalance
	}

	now := time.Now().UTC()
	return &Wallet{
		id:        uuid.New(),
		playerID:  playerID,
		currency:  initialBalance.Currency(),
		balance:   initialBalance,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// Rehydrate reconstructs an existing Wallet aggregate from persistence.
// It must NOT emit events, increment versions or apply validations meant for new entities.
func Rehydrate(
	id uuid.UUID,
	playerID uuid.UUID,
	currency string,
	balance money.Money,
	version int64,
	createdAt time.Time,
	updatedAt time.Time,
) (*Wallet, error) {
	if id == uuid.Nil {
		return nil, ErrInvalidWalletID
	}
	if playerID == uuid.Nil {
		return nil, ErrInvalidPlayerID
	}
	if balance.Currency() != currency {
		return nil, money.ErrCurrencyMismatch
	}
	if balance.IsNegative() {
		return nil, ErrInsufficientBalance
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  currency,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

// ID returns the wallet unique identifier.
func (w *Wallet) ID() uuid.UUID {
	return w.id
}

// PlayerID returns the owner player identifier.
func (w *Wallet) PlayerID() uuid.UUID {
	return w.playerID
}

// Currency returns the wallet currency code.
func (w *Wallet) Currency() string {
	return w.currency
}

// Balance returns the current wallet balance.
func (w *Wallet) Balance() money.Money {
	return w.balance
}

// Version returns the current version for concurrency control.
func (w *Wallet) Version() int64 {
	return w.version
}

// CreatedAt returns the wallet creation timestamp.
func (w *Wallet) CreatedAt() time.Time {
	return w.createdAt
}

// UpdatedAt returns the timestamp of the last balance change.
func (w *Wallet) UpdatedAt() time.Time {
	return w.updatedAt
}

// Debit deducts money from the wallet.
// Invariant: Balance after deduction must be >= 0.
// On success, increments version by 1 and updates updatedAt.
func (w *Wallet) Debit(amount money.Money) (before money.Money, after money.Money, err error) {
	if !amount.IsPositive() {
		return w.balance, w.balance, ErrInvalidAmount
	}
	if amount.Currency() != w.currency {
		return w.balance, w.balance, money.ErrCurrencyMismatch
	}

	cmp, err := w.balance.Compare(amount)
	if err != nil {
		return w.balance, w.balance, err
	}
	if cmp < 0 {
		return w.balance, w.balance, ErrInsufficientBalance
	}

	before = w.balance
	newBalance, err := w.balance.Sub(amount)
	if err != nil {
		return w.balance, w.balance, err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	after = w.balance

	return before, after, nil
}

// Credit adds money to the wallet.
// On success, increments version by 1 and updates updatedAt.
func (w *Wallet) Credit(amount money.Money) (before money.Money, after money.Money, err error) {
	if !amount.IsPositive() {
		return w.balance, w.balance, ErrInvalidAmount
	}
	if amount.Currency() != w.currency {
		return w.balance, w.balance, money.ErrCurrencyMismatch
	}

	before = w.balance
	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return w.balance, w.balance, err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	after = w.balance

	return before, after, nil
}
