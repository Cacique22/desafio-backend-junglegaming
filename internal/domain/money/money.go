package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	ErrInvalidCurrency     = errors.New("currency must be a 3-letter uppercase ISO 4217 code")
	ErrCurrencyMismatch    = errors.New("currency mismatch between money values")
	ErrInvalidAmountFormat = errors.New("amount must be a valid decimal string with exactly or up to 2 decimal places")
	ErrNegativeNotAllowed  = errors.New("negative amount is not allowed in external financial input")
	ErrAmountOverflow      = errors.New("arithmetic operation caused int64 overflow")
	ErrInvalidValue        = errors.New("amount cannot be empty, NaN, Infinity or scientific notation")
)

// Money is an immutable value object representing a monetary amount in cents (minimal units)
// and an ISO 4217 currency.
// CRITICAL: float32/float64 is strictly forbidden in parsing, math, serialization, and storage.
type Money struct {
	cents    int64  // Represented in cents (e.g. 25.00 BRL = 2500 cents)
	currency string // ISO 4217 code (e.g. "BRL", "USD", "EUR")
}

// Zero returns a Money value of 0.00 for the given currency.
func Zero(currency string) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{cents: 0, currency: currency}, nil
}

// MustZero returns a Money value of 0.00 or panics if the currency is invalid.
func MustZero(currency string) Money {
	m, err := Zero(currency)
	if err != nil {
		panic(err)
	}
	return m
}

// FromCents creates a Money instance directly from cents (int64) and currency.
// Primarily used when rehydrating from database or internal calculations.
func FromCents(cents int64, currency string) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{cents: cents, currency: currency}, nil
}

// Parse parses a decimal string like "25.00" and currency into Money.
// Rejects float notation, NaN, Infinity, exponents, and scale > 2.
// allowNegative controls whether negative values are permitted (e.g. internal differences vs external input).
func Parse(amountStr string, currency string, allowNegative bool) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}

	trimmed := strings.TrimSpace(amountStr)
	if trimmed == "" {
		return Money{}, ErrInvalidValue
	}

	// Reject NaN, Infinity, scientific notation
	upper := strings.ToUpper(trimmed)
	if strings.Contains(upper, "NAN") || strings.Contains(upper, "INF") || strings.Contains(upper, "E") {
		return Money{}, ErrInvalidValue
	}

	isNegative := false
	if strings.HasPrefix(trimmed, "-") {
		if !allowNegative {
			return Money{}, ErrNegativeNotAllowed
		}
		isNegative = true
		trimmed = trimmed[1:]
	} else if strings.HasPrefix(trimmed, "+") {
		trimmed = trimmed[1:]
	}

	if trimmed == "" {
		return Money{}, ErrInvalidAmountFormat
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) > 2 {
		return Money{}, ErrInvalidAmountFormat
	}

	intPartStr := parts[0]
	decPartStr := ""
	if len(parts) == 2 {
		decPartStr = parts[1]
	}

	// Validate integer part
	if intPartStr == "" {
		intPartStr = "0"
	}
	for _, ch := range intPartStr {
		if ch < '0' || ch > '9' {
			return Money{}, ErrInvalidAmountFormat
		}
	}

	// Validate decimal part (must have at most 2 digits)
	if len(decPartStr) > 2 {
		return Money{}, ErrInvalidAmountFormat
	}
	for _, ch := range decPartStr {
		if ch < '0' || ch > '9' {
			return Money{}, ErrInvalidAmountFormat
		}
	}

	// Normalize decimal part to exactly 2 digits
	for len(decPartStr) < 2 {
		decPartStr += "0"
	}

	// Parse integer part as int64 with overflow check
	var intVal int64
	for _, ch := range intPartStr {
		digit := int64(ch - '0')
		if intVal > (math.MaxInt64-digit)/10 {
			return Money{}, ErrAmountOverflow
		}
		intVal = intVal*10 + digit
	}

	// Multiply intVal by 100 to get cents
	if intVal > math.MaxInt64/100 {
		return Money{}, ErrAmountOverflow
	}
	cents := intVal * 100

	// Add decimal cents
	decVal := int64((decPartStr[0]-'0')*10 + (decPartStr[1] - '0'))
	if cents > math.MaxInt64-decVal {
		return Money{}, ErrAmountOverflow
	}
	cents += decVal

	if isNegative {
		cents = -cents
	}

	return Money{cents: cents, currency: currency}, nil
}

// Cents returns the raw amount in cents.
func (m Money) Cents() int64 {
	return m.cents
}

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string {
	return m.currency
}

// String returns formatted decimal string e.g. "25.00".
func (m Money) String() string {
	sign := ""
	val := m.cents
	if val < 0 {
		sign = "-"
		val = -val
	}
	intPart := val / 100
	decPart := val % 100
	return fmt.Sprintf("%s%d.%02d", sign, intPart, decPart)
}

// AmountString returns formatted decimal string without currency e.g. "25.00".
func (m Money) AmountString() string {
	return m.String()
}

// Add adds two Money values. Returns ErrCurrencyMismatch or ErrAmountOverflow if applicable.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	// Overflow check for a + b
	a, b := m.cents, other.cents
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return Money{}, ErrAmountOverflow
	}
	return Money{cents: a + b, currency: m.currency}, nil
}

// Sub subtracts other Money from m.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	a, b := m.cents, other.cents
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return Money{}, ErrAmountOverflow
	}
	return Money{cents: a - b, currency: m.currency}, nil
}

// Neg returns the negated Money value.
func (m Money) Neg() (Money, error) {
	if m.cents == math.MinInt64 {
		return Money{}, ErrAmountOverflow
	}
	return Money{cents: -m.cents, currency: m.currency}, nil
}

// Compare returns -1 if m < other, 0 if m == other, 1 if m > other.
func (m Money) Compare(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, ErrCurrencyMismatch
	}
	if m.cents < other.cents {
		return -1, nil
	}
	if m.cents > other.cents {
		return 1, nil
	}
	return 0, nil
}

// IsZero returns true if cents == 0.
func (m Money) IsZero() bool {
	return m.cents == 0
}

// IsPositive returns true if cents > 0.
func (m Money) IsPositive() bool {
	return m.cents > 0
}

// IsNegative returns true if cents < 0.
func (m Money) IsNegative() bool {
	return m.cents < 0
}

// GreaterThan returns true if m > other.
func (m Money) GreaterThan(other Money) (bool, error) {
	cmp, err := m.Compare(other)
	return cmp > 0, err
}

// GreaterThanOrEqual returns true if m >= other.
func (m Money) GreaterThanOrEqual(other Money) (bool, error) {
	cmp, err := m.Compare(other)
	return cmp >= 0, err
}

// LessThan returns true if m < other.
func (m Money) LessThan(other Money) (bool, error) {
	cmp, err := m.Compare(other)
	return cmp < 0, err
}

// Equals returns true if currency and cents are equal.
func (m Money) Equals(other Money) bool {
	return m.currency == other.currency && m.cents == other.cents
}

// JSONDTO represents the external JSON structure: {"amount":"25.00","currency":"BRL"}
type JSONDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON serializes Money as {"amount":"25.00","currency":"BRL"} without float usage.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(JSONDTO{
		Amount:   m.AmountString(),
		Currency: m.currency,
	})
}

// UnmarshalJSON deserializes {"amount":"25.00","currency":"BRL"} strictly without float.
func (m *Money) UnmarshalJSON(data []byte) error {
	var dto JSONDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return err
	}
	// For external incoming contracts, negative amounts are rejected by default
	parsed, err := Parse(dto.Amount, dto.Currency, false)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func validateCurrency(c string) error {
	c = strings.TrimSpace(c)
	if len(c) != 3 {
		return ErrInvalidCurrency
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return ErrInvalidCurrency
		}
	}
	return nil
}
