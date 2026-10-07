package money_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

func TestMoney_Parse_Valid(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		currency      string
		expectedCents int64
		expectedStr   string
	}{
		{"integer only", "25", "BRL", 2500, "25.00"},
		{"one decimal place", "25.5", "BRL", 2550, "25.50"},
		{"two decimal places", "25.00", "BRL", 2500, "25.00"},
		{"cents only", "0.99", "USD", 99, "0.99"},
		{"zero", "0.00", "BRL", 0, "0.00"},
		{"large value", "1000000.50", "BRL", 100000050, "1000000.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := money.Parse(tt.input, tt.currency, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Cents() != tt.expectedCents {
				t.Errorf("expected cents %d, got %d", tt.expectedCents, m.Cents())
			}
			if m.String() != tt.expectedStr {
				t.Errorf("expected string %s, got %s", tt.expectedStr, m.String())
			}
			if m.Currency() != tt.currency {
				t.Errorf("expected currency %s, got %s", tt.currency, m.Currency())
			}
		})
	}
}

func TestMoney_Parse_Invalid(t *testing.T) {
	invalidInputs := []struct {
		name     string
		input    string
		currency string
	}{
		{"empty amount", "", "BRL"},
		{"extra scale 3 decimals", "25.001", "BRL"},
		{"scientific notation lowercase", "1e5", "BRL"},
		{"scientific notation uppercase", "1E5", "BRL"},
		{"NaN", "NaN", "BRL"},
		{"Infinity", "Infinity", "BRL"},
		{"Inf", "Inf", "BRL"},
		{"invalid characters", "25.00a", "BRL"},
		{"negative rejected in external mode", "-10.00", "BRL"},
		{"invalid currency lowercase", "10.00", "brl"},
		{"invalid currency length", "10.00", "BRLX"},
		{"empty currency", "10.00", ""},
	}

	for _, tt := range invalidInputs {
		t.Run(tt.name, func(t *testing.T) {
			_, err := money.Parse(tt.input, tt.currency, false)
			if err == nil {
				t.Errorf("expected error for input %q currency %q, but got nil", tt.input, tt.currency)
			}
		})
	}
}

func TestMoney_Parse_AllowNegative(t *testing.T) {
	m, err := money.Parse("-50.25", "BRL", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Cents() != -5025 {
		t.Errorf("expected cents -5025, got %d", m.Cents())
	}
	if m.String() != "-50.25" {
		t.Errorf("expected string '-50.25', got %s", m.String())
	}
	if !m.IsNegative() {
		t.Error("expected IsNegative to be true")
	}
}

func TestMoney_Arithmetic(t *testing.T) {
	m1, _ := money.Parse("100.50", "BRL", false)
	m2, _ := money.Parse("49.50", "BRL", false)
	mUSD, _ := money.Parse("10.00", "USD", false)

	// Addition
	sum, err := m1.Add(m2)
	if err != nil {
		t.Fatalf("unexpected add error: %v", err)
	}
	if sum.Cents() != 15000 || sum.String() != "150.00" {
		t.Errorf("expected 150.00, got %s", sum.String())
	}

	// Subtraction
	diff, err := m1.Sub(m2)
	if err != nil {
		t.Fatalf("unexpected sub error: %v", err)
	}
	if diff.Cents() != 5100 || diff.String() != "51.00" {
		t.Errorf("expected 51.00, got %s", diff.String())
	}

	// Currency Mismatch on Add
	_, err = m1.Add(mUSD)
	if err != money.ErrCurrencyMismatch {
		t.Errorf("expected ErrCurrencyMismatch, got %v", err)
	}

	// Currency Mismatch on Sub
	_, err = m1.Sub(mUSD)
	if err != money.ErrCurrencyMismatch {
		t.Errorf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestMoney_Overflow(t *testing.T) {
	maxMoney, _ := money.FromCents(math.MaxInt64-10, "BRL")
	tenCents, _ := money.FromCents(20, "BRL")

	_, err := maxMoney.Add(tenCents)
	if err != money.ErrAmountOverflow {
		t.Errorf("expected ErrAmountOverflow on add, got %v", err)
	}

	minMoney, _ := money.FromCents(math.MinInt64+10, "BRL")
	_, err = minMoney.Sub(tenCents)
	if err != money.ErrAmountOverflow {
		t.Errorf("expected ErrAmountOverflow on sub, got %v", err)
	}
}

func TestMoney_JSON_Roundtrip(t *testing.T) {
	original, err := money.Parse("123.45", "BRL", false)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	expectedJSON := `{"amount":"123.45","currency":"BRL"}`
	if string(data) != expectedJSON {
		t.Errorf("expected JSON %s, got %s", expectedJSON, string(data))
	}

	var deserialized money.Money
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if !original.Equals(deserialized) {
		t.Errorf("expected %v to equal %v", deserialized, original)
	}
}
