package wager_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
)

func TestTransaction_NewExternal_Valid(t *testing.T) {
	m, _ := money.Parse("25.00", "BRL", false)
	tx, err := wager.NewExternalTransaction(
		"provider-a",
		"tx-1",
		"key-1",
		"hash-1",
		uuid.New(),
		uuid.New(),
		"round-1",
		"game-1",
		wager.KindBet,
		m,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error creating bet transaction: %v", err)
	}

	if tx.Status() != wager.StatusPending {
		t.Errorf("expected initial status PENDING, got %s", tx.Status())
	}
	if tx.Kind() != wager.KindBet {
		t.Errorf("expected kind BET, got %s", tx.Kind())
	}
}

func TestTransaction_OpeningRejectedExternally(t *testing.T) {
	m, _ := money.Parse("100.00", "BRL", false)
	_, err := wager.NewExternalTransaction(
		"provider-a",
		"tx-1",
		"key-1",
		"hash-1",
		uuid.New(),
		uuid.New(),
		"round-1",
		"game-1",
		wager.KindOpening,
		m,
		nil,
	)
	if err != wager.ErrOpeningNotAllowedExternal {
		t.Errorf("expected ErrOpeningNotAllowedExternal, got %v", err)
	}
}

func TestTransaction_ReversalRequiresReference(t *testing.T) {
	m, _ := money.Parse("50.00", "BRL", false)
	_, err := wager.NewExternalTransaction(
		"provider-a",
		"tx-1",
		"key-1",
		"hash-1",
		uuid.New(),
		uuid.New(),
		"round-1",
		"game-1",
		wager.KindRefund,
		m,
		nil,
	)
	if err != wager.ErrMissingReferenceForReversal {
		t.Errorf("expected ErrMissingReferenceForReversal, got %v", err)
	}
}

func TestTransaction_LossRequiresZeroAmount(t *testing.T) {
	nonZero, _ := money.Parse("10.00", "BRL", false)
	_, err := wager.NewExternalTransaction(
		"provider-a",
		"tx-1",
		"key-1",
		"hash-1",
		uuid.New(),
		uuid.New(),
		"round-1",
		"game-1",
		wager.KindLoss,
		nonZero,
		nil,
	)
	if err != wager.ErrLossMustBeZero {
		t.Errorf("expected ErrLossMustBeZero, got %v", err)
	}

	zeroMoney, _ := money.Zero("BRL")
	tx, err := wager.NewExternalTransaction(
		"provider-a",
		"tx-1",
		"key-1",
		"hash-1",
		uuid.New(),
		uuid.New(),
		"round-1",
		"game-1",
		wager.KindLoss,
		zeroMoney,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error creating valid loss: %v", err)
	}
	if tx.Kind() != wager.KindLoss {
		t.Errorf("expected kind LOSS, got %s", tx.Kind())
	}
}

func TestTransaction_TerminalStateImmutability(t *testing.T) {
	m, _ := money.Parse("25.00", "BRL", false)
	tx, _ := wager.NewExternalTransaction(
		"provider-a", "tx-1", "key-1", "hash-1",
		uuid.New(), uuid.New(), "round-1", "game-1",
		wager.KindBet, m, nil,
	)

	finalBal, _ := money.Parse("75.00", "BRL", false)
	if err := tx.MarkProcessed(finalBal); err != nil {
		t.Fatalf("failed to transition to PROCESSED: %v", err)
	}

	// Attempting to reject already processed transaction
	err := tx.MarkRejected(wager.FailureCodeInsufficientFunds)
	if err != wager.ErrTerminalStateImmutable {
		t.Errorf("expected ErrTerminalStateImmutable, got %v", err)
	}
}
