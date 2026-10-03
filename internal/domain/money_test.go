package domain

import (
	"errors"
	"math"
	"testing"
)

func TestNewMoney(t *testing.T) {
	money, err := NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if money.MinorUnits() != 2500 {
		t.Fatalf(
			"expected 2500 minor units, got %d",
			money.MinorUnits(),
		)
	}

	if money.Currency() != "BRL" {
		t.Fatalf(
			"expected BRL, got %s",
			money.Currency(),
		)
	}

	if money.Amount() != "25.00" {
		t.Fatalf(
			"expected 25.00, got %s",
			money.Amount(),
		)
	}
}

func TestNewMoneyRejectsInvalidAmounts(t *testing.T) {
	tests := []string{
		"",
		"25",
		"25.0",
		"25.000",
		"-25.00",
		"+25.00",
		"NaN",
		"Infinity",
		"Inf",
		"2.5e1",
		"01.00",
		".50",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := NewMoney(input, "BRL")

			if !errors.Is(err, ErrInvalidMoneyAmount) {
				t.Fatalf(
					"expected ErrInvalidMoneyAmount for %q, got %v",
					input,
					err,
				)
			}
		})
	}
}

func TestNewMoneyRejectsInvalidCurrency(t *testing.T) {
	tests := []string{
		"",
		"BR",
		"BRLL",
		"brl",
		"123",
	}

	for _, currency := range tests {
		t.Run(currency, func(t *testing.T) {
			_, err := NewMoney("10.00", currency)

			if !errors.Is(err, ErrInvalidCurrency) {
				t.Fatalf(
					"expected ErrInvalidCurrency for %q, got %v",
					currency,
					err,
				)
			}
		})
	}
}

func TestZeroMoney(t *testing.T) {
	money, err := ZeroMoney("BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !money.IsZero() {
		t.Fatal("expected zero money")
	}

	if money.Amount() != "0.00" {
		t.Fatalf(
			"expected 0.00, got %s",
			money.Amount(),
		)
	}
}

func TestMoneyAdd(t *testing.T) {
	left, _ := NewMoney("10.25", "BRL")
	right, _ := NewMoney("5.75", "BRL")

	result, err := left.Add(right)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Amount() != "16.00" {
		t.Fatalf(
			"expected 16.00, got %s",
			result.Amount(),
		)
	}
}

func TestMoneySubtract(t *testing.T) {
	left, _ := NewMoney("10.00", "BRL")
	right, _ := NewMoney("25.00", "BRL")

	result, err := left.Subtract(right)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Amount() != "-15.00" {
		t.Fatalf(
			"expected -15.00, got %s",
			result.Amount(),
		)
	}

	if !result.IsNegative() {
		t.Fatal("expected negative result")
	}
}

func TestMoneyRejectsCurrencyMismatch(t *testing.T) {
	brl, _ := NewMoney("10.00", "BRL")
	usd, _ := NewMoney("10.00", "USD")

	_, err := brl.Add(usd)

	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf(
			"expected ErrCurrencyMismatch, got %v",
			err,
		)
	}
}

func TestMoneyCompare(t *testing.T) {
	first, _ := NewMoney("10.00", "BRL")
	second, _ := NewMoney("20.00", "BRL")

	result, err := first.Compare(second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != -1 {
		t.Fatalf("expected -1, got %d", result)
	}
}

func TestMoneyNegate(t *testing.T) {
	money, _ := NewMoney("25.00", "BRL")

	result, err := money.Negate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Amount() != "-25.00" {
		t.Fatalf(
			"expected -25.00, got %s",
			result.Amount(),
		)
	}
}

func TestMoneyParsingOverflow(t *testing.T) {
	_, err := NewMoney(
		"92233720368547759.00",
		"BRL",
	)

	if !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf(
			"expected ErrMoneyOverflow, got %v",
			err,
		)
	}
}

func TestMoneyAdditionOverflow(t *testing.T) {
	left, err := MoneyFromMinorUnits(
		math.MaxInt64,
		"BRL",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	right, _ := MoneyFromMinorUnits(1, "BRL")

	_, err = left.Add(right)

	if !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf(
			"expected ErrMoneyOverflow, got %v",
			err,
		)
	}
}

func TestMoneyNegateOverflow(t *testing.T) {
	money, err := MoneyFromMinorUnits(
		math.MinInt64,
		"BRL",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = money.Negate()

	if !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf(
			"expected ErrMoneyOverflow, got %v",
			err,
		)
	}
}
