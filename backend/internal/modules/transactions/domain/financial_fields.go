package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFinancialDescriptionRunes = 200
	// MaxIdentifierBytes bounds opaque technical identifiers by UTF-8 bytes.
	MaxIdentifierBytes = 128
)

// TransactionType identifies the financial meaning of a transaction.
type TransactionType string

const (
	TransactionTypeExpense TransactionType = "EXPENSE"
	TransactionTypeIncome  TransactionType = "INCOME"
)

// Origin identifies the channel that confirmed a financial record command.
type Origin string

const (
	OriginIOS      Origin = "IOS"
	OriginWhatsApp Origin = "WHATSAPP"
)

func normalizeFinancialDescription(value string) (string, bool) {
	description := strings.TrimSpace(value)
	if description == "" || !utf8.ValidString(description) || utf8.RuneCountInString(description) > maxFinancialDescriptionRunes {
		return "", false
	}
	return description, true
}

func isValidIdentifier(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > MaxIdentifierBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func isValidTimezone(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value == "Local" {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

func normalizeInstant(value time.Time) time.Time {
	return value.UTC().Round(0)
}

func (origin Origin) valid() bool {
	switch origin {
	case OriginIOS, OriginWhatsApp:
		return true
	default:
		return false
	}
}
