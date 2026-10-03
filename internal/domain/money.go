package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidMoneyAmount = errors.New("invalid money amount")
	ErrInvalidCurrency    = errors.New("invalid currency")
	ErrCurrencyMismatch   = errors.New("currency mismatch")
	ErrMoneyOverflow      = errors.New("money overflow")
)

var (
	moneyPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.[0-9]{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Money struct {
	minorUnits int64
	currency   string
}

func NewMoney(amount string, currency string) (Money, error) {
	if !currencyPattern.MatchString(currency) {
		return Money{}, ErrInvalidCurrency
	}

	minorUnits, err := parseMinorUnits(amount)
	if err != nil {
		return Money{}, err
	}

	return Money{
		minorUnits: minorUnits,
		currency:   currency,
	}, nil
}

func ZeroMoney(currency string) (Money, error) {
	return NewMoney("0.00", currency)
}

func MoneyFromMinorUnits(minorUnits int64, currency string) (Money, error) {
	if !currencyPattern.MatchString(currency) {
		return Money{}, ErrInvalidCurrency
	}

	return Money{
		minorUnits: minorUnits,
		currency:   currency,
	}, nil
}

func parseMinorUnits(amount string) (int64, error) {
	if !moneyPattern.MatchString(amount) {
		return 0, ErrInvalidMoneyAmount
	}

	parts := strings.Split(amount, ".")

	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, ErrInvalidMoneyAmount
	}

	fraction, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, ErrInvalidMoneyAmount
	}

	maxWhole := uint64(math.MaxInt64 / 100)

	if whole > maxWhole {
		return 0, ErrMoneyOverflow
	}

	minorUnits := whole * 100

	if minorUnits > uint64(math.MaxInt64)-fraction {
		return 0, ErrMoneyOverflow
	}

	return int64(minorUnits + fraction), nil
}

func (m Money) MinorUnits() int64 {
	return m.minorUnits
}

func (m Money) Currency() string {
	return m.currency
}

func (m Money) Amount() string {
	negative := m.minorUnits < 0

	var absolute uint64
	if negative {
		absolute = uint64(-(m.minorUnits + 1)) + 1
	} else {
		absolute = uint64(m.minorUnits)
	}

	whole := absolute / 100
	fraction := absolute % 100

	if negative {
		return fmt.Sprintf("-%d.%02d", whole, fraction)
	}

	return fmt.Sprintf("%d.%02d", whole, fraction)
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.ensureCompatible(other); err != nil {
		return Money{}, err
	}

	if other.minorUnits > 0 && m.minorUnits > math.MaxInt64-other.minorUnits {
		return Money{}, ErrMoneyOverflow
	}

	if other.minorUnits < 0 && m.minorUnits < math.MinInt64-other.minorUnits {
		return Money{}, ErrMoneyOverflow
	}

	return Money{
		minorUnits: m.minorUnits + other.minorUnits,
		currency:   m.currency,
	}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	negated, err := other.Negate()
	if err != nil {
		return Money{}, err
	}

	return m.Add(negated)
}

func (m Money) Negate() (Money, error) {
	if m.minorUnits == math.MinInt64 {
		return Money{}, ErrMoneyOverflow
	}

	return Money{
		minorUnits: -m.minorUnits,
		currency:   m.currency,
	}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if err := m.ensureCompatible(other); err != nil {
		return 0, err
	}

	switch {
	case m.minorUnits < other.minorUnits:
		return -1, nil
	case m.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) IsZero() bool {
	return m.minorUnits == 0
}

func (m Money) IsPositive() bool {
	return m.minorUnits > 0
}

func (m Money) IsNegative() bool {
	return m.minorUnits < 0
}

func (m Money) Equal(other Money) bool {
	return m.currency == other.currency &&
		m.minorUnits == other.minorUnits
}

func (m Money) ensureCompatible(other Money) error {
	if m.currency == "" || other.currency == "" {
		return ErrInvalidCurrency
	}

	if m.currency != other.currency {
		return ErrCurrencyMismatch
	}

	return nil
}
