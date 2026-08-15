package application

import (
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestFingerprintIncomeUsesIncomeTypeAsDomainSeparator(t *testing.T) {
	amount, err := domain.NewMoney(725000, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	details, err := domain.NormalizeIncomeDetails(domain.IncomeDetails{
		UserID:            "user-synthetic-001",
		Description:       "Receita sintética",
		Amount:            amount,
		OccurredAt:        time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC),
		FinancialTimezone: FinancialTimezone,
		Origin:            domain.OriginIOS,
	})
	if err != nil {
		t.Fatalf("NormalizeIncomeDetails() error = %v", err)
	}

	incomeFingerprint := fingerprintIncome(details)
	expenseTypedDigest := newRequestFingerprintDigest()
	writeFingerprintString(expenseTypedDigest, string(domain.TransactionTypeExpense))
	writeFingerprintString(expenseTypedDigest, details.Description)
	writeFingerprintInt64(expenseTypedDigest, details.Amount.MinorUnits())
	writeFingerprintString(expenseTypedDigest, string(details.Amount.Currency()))
	writeFingerprintString(expenseTypedDigest, details.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	writeFingerprintString(expenseTypedDigest, details.FinancialTimezone)
	writeFingerprintString(expenseTypedDigest, string(details.Origin))
	var expenseTypedFingerprint RequestFingerprint
	copy(expenseTypedFingerprint[:], expenseTypedDigest.Sum(nil))

	if incomeFingerprint == expenseTypedFingerprint {
		t.Fatal("changing only the fingerprint type discriminator did not change the digest")
	}
}
