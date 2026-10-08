package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

const (
	TypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	TypeWagerTransactionRejected         = "WagerTransactionRejected"
	TypeWalletBalanceChanged             = "WalletBalanceChanged"
	TypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

// Envelope is the standard envelope for all domain events.
type Envelope struct {
	EventID       uuid.UUID       `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   *string         `json:"causationId,omitempty"`
	OccurredAt    string          `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
}

// NewEnvelope creates a new typed event envelope with UTC RFC 3339 timestamp.
func NewEnvelope(eventType string, aggregateID string, correlationID string, causationID *string, version int, payload interface{}) (*Envelope, error) {
	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		EventID:       uuid.New(),
		EventType:     eventType,
		AggregateID:   aggregateID,
		CorrelationID: correlationID,
		CausationID:   causationID,
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Version:       version,
		Data:          dataBytes,
	}, nil
}

// WalletBalanceChangedPayload represents the mandatory payload for balance changes.
type WalletBalanceChangedPayload struct {
	WalletID      string      `json:"walletId"`
	TransactionID string      `json:"transactionId"`
	Direction     string      `json:"direction"`
	Money         money.Money `json:"money"`
	BalanceBefore string      `json:"balanceBefore"`
	BalanceAfter  string      `json:"balanceAfter"`
	WalletVersion int64       `json:"walletVersion"`
}

// WagerTransactionProcessedPayload represents the payload when a wager completes.
type WagerTransactionProcessedPayload struct {
	TransactionID string      `json:"transactionId"`
	ProviderID    string      `json:"providerId"`
	PlayerID      string      `json:"playerId"`
	WalletID      string      `json:"walletId"`
	RoundID       string      `json:"roundId"`
	GameID        string      `json:"gameId"`
	Kind          string      `json:"kind"`
	Money         money.Money `json:"money"`
	Status        string      `json:"status"`
	FinalBalance  *string     `json:"finalBalance,omitempty"`
}

// WagerTransactionRejectedPayload represents the payload when a wager is rejected.
type WagerTransactionRejectedPayload struct {
	TransactionID string      `json:"transactionId"`
	ProviderID    string      `json:"providerId"`
	FailureCode   string      `json:"failureCode"`
	Reason        string      `json:"reason"`
	Kind          string      `json:"kind"`
	Money         money.Money `json:"money"`
}

// WagerTransactionPendingReferencePayload represents waiting for reference.
type WagerTransactionPendingReferencePayload struct {
	TransactionID                  string      `json:"transactionId"`
	ProviderID                     string      `json:"providerId"`
	ReferenceExternalTransactionID string      `json:"referenceExternalTransactionId"`
	Kind                           string      `json:"kind"`
	Money                          money.Money `json:"money"`
}
