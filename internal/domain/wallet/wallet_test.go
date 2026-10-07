package wallet_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wallet"
)

func TestWallet_CreationAndInvariants(t *testing.T) {
	playerID := uuid.New()
	initialBalance, _ := money.Parse("100.00", "BRL", false)

	w, err := wallet.New(playerID, initialBalance)
	if err != nil {
		t.Fatalf("unexpected error creating wallet: %v", err)
	}

	if w.ID() == uuid.Nil {
		t.Error("expected non-nil wallet ID")
	}
	if w.PlayerID() != playerID {
		t.Errorf("expected player ID %s, got %s", playerID, w.PlayerID())
	}
	if !w.Balance().Equals(initialBalance) {
		t.Errorf("expected balance %v, got %v", initialBalance, w.Balance())
	}
	if w.Version() != 1 {
		t.Errorf("expected initial version 1, got %d", w.Version())
	}
}

func TestWallet_Debit_SuccessAndInsufficient(t *testing.T) {
	playerID := uuid.New()
	initial, _ := money.Parse("100.00", "BRL", false)
	w, _ := wallet.New(playerID, initial)

	// Successful debit of 80.00
	debitAmount, _ := money.Parse("80.00", "BRL", false)
	before, after, err := w.Debit(debitAmount)
	if err != nil {
		t.Fatalf("unexpected debit error: %v", err)
	}
	if before.String() != "100.00" || after.String() != "20.00" {
		t.Errorf("expected before 100.00 and after 20.00, got %s and %s", before.String(), after.String())
	}
	if w.Version() != 2 {
		t.Errorf("expected version 2 after debit, got %d", w.Version())
	}

	// Insufficient balance: attempt second debit of 80.00 (only 20.00 available)
	_, _, err = w.Debit(debitAmount)
	if err != wallet.ErrInsufficientBalance {
		t.Errorf("expected ErrInsufficientBalance, got %v", err)
	}
	if w.Version() != 2 {
		t.Errorf("version should not increment on failed debit, got %d", w.Version())
	}
	if w.Balance().String() != "20.00" {
		t.Errorf("balance should remain 20.00, got %s", w.Balance().String())
	}
}

func TestWallet_Credit(t *testing.T) {
	playerID := uuid.New()
	initial, _ := money.Parse("50.00", "BRL", false)
	w, _ := wallet.New(playerID, initial)

	creditAmount, _ := money.Parse("25.00", "BRL", false)
	before, after, err := w.Credit(creditAmount)
	if err != nil {
		t.Fatalf("unexpected credit error: %v", err)
	}
	if before.String() != "50.00" || after.String() != "75.00" {
		t.Errorf("expected 50.00 -> 75.00, got %s -> %s", before.String(), after.String())
	}
	if w.Version() != 2 {
		t.Errorf("expected version 2 after credit, got %d", w.Version())
	}
}

func TestLedgerEntry_Validation(t *testing.T) {
	walletID := uuid.New()
	txID := uuid.New()

	before, _ := money.Parse("100.00", "BRL", false)
	amount, _ := money.Parse("30.00", "BRL", false)
	afterDebit, _ := money.Parse("70.00", "BRL", false)
	wrongAfter, _ := money.Parse("60.00", "BRL", false)

	// Valid debit
	entry, err := wallet.NewLedgerEntry(walletID, txID, wallet.DirectionDebit, amount, before, afterDebit)
	if err != nil {
		t.Fatalf("unexpected ledger error: %v", err)
	}
	if entry.Direction() != wallet.DirectionDebit {
		t.Errorf("expected DEBIT direction, got %s", entry.Direction())
	}

	// Invalid debit math
	_, err = wallet.NewLedgerEntry(walletID, txID, wallet.DirectionDebit, amount, before, wrongAfter)
	if err != wallet.ErrInvalidLedgerMath {
		t.Errorf("expected ErrInvalidLedgerMath, got %v", err)
	}
}
