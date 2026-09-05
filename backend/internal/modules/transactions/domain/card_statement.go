package domain

import (
	"errors"
	"math"
	"sort"
	"time"
)

// CardStatementPurchaseMode identifies how a confirmed card purchase is
// represented in a statement. It is a read-model value, not a new financial
// lifecycle.
type CardStatementPurchaseMode string

const (
	CardStatementPurchaseModeOneTime     CardStatementPurchaseMode = "ONE_TIME"
	CardStatementPurchaseModeInstallment CardStatementPurchaseMode = "INSTALLMENT"
)

var (
	ErrInvalidCardStatementCreditCardID    = errors.New("card statement: invalid credit card id")
	ErrInvalidCardStatementDueDate         = errors.New("card statement: invalid statement due date")
	ErrInvalidCardStatementLineExpenseID   = errors.New("card statement: invalid expense id")
	ErrInvalidCardStatementLineDescription = errors.New("card statement: invalid line description")
	ErrInvalidCardStatementLineAmount      = errors.New("card statement: invalid line amount")
	ErrInvalidCardStatementLineOccurredAt  = errors.New("card statement: invalid line occurrence time")
	ErrInvalidCardStatementPurchaseMode    = errors.New("card statement: invalid purchase mode")
	ErrInvalidCardStatementInstallment     = errors.New("card statement: invalid installment metadata")
	ErrInvalidCardStatementAmount          = errors.New("card statement: invalid total amount")
	ErrCardStatementAmountOverflow         = errors.New("card statement: total amount overflow")
	ErrCardStatementTotalMismatch          = errors.New("card statement: total does not match lines")
	ErrCardStatementDuplicateLine          = errors.New("card statement: duplicate line")
)

// CardStatementLineParams contains one occurred purchase or one installment
// due in a statement cycle. Installment metadata is present only for a
// parcelled purchase; one-time purchases use the canonical nil representation.
type CardStatementLineParams struct {
	ExpenseID         string
	Description       string
	Amount            Money
	OccurredAt        time.Time
	PurchaseMode      CardStatementPurchaseMode
	InstallmentNumber *int
	InstallmentCount  *int
}

// CardStatementLine is an immutable, derived statement line. It never
// represents a future Expense or an independently persisted installment.
type CardStatementLine struct {
	expenseID         string
	description       string
	amount            Money
	occurredAt        time.Time
	purchaseMode      CardStatementPurchaseMode
	installmentNumber int
	hasInstallmentNum bool
	installmentCount  int
	hasInstallmentCnt bool
}

func NewCardStatementLine(params CardStatementLineParams) (CardStatementLine, error) {
	if !isValidIdentifier(params.ExpenseID) {
		return CardStatementLine{}, ErrInvalidCardStatementLineExpenseID
	}
	description, valid := normalizeFinancialDescription(params.Description)
	if !valid {
		return CardStatementLine{}, ErrInvalidCardStatementLineDescription
	}
	if params.Amount.Currency() != CurrencyBRL || params.Amount.MinorUnits() <= 0 {
		return CardStatementLine{}, ErrInvalidCardStatementLineAmount
	}
	if params.OccurredAt.IsZero() {
		return CardStatementLine{}, ErrInvalidCardStatementLineOccurredAt
	}

	line := CardStatementLine{
		expenseID: params.ExpenseID, description: description,
		amount: params.Amount, occurredAt: normalizeInstant(params.OccurredAt),
		purchaseMode: params.PurchaseMode,
	}
	switch params.PurchaseMode {
	case CardStatementPurchaseModeOneTime:
		if params.InstallmentNumber != nil || params.InstallmentCount != nil {
			return CardStatementLine{}, ErrInvalidCardStatementInstallment
		}
	case CardStatementPurchaseModeInstallment:
		if params.InstallmentNumber == nil || params.InstallmentCount == nil {
			return CardStatementLine{}, ErrInvalidCardStatementInstallment
		}
		if *params.InstallmentCount < MinInstallmentCount || *params.InstallmentCount > MaxInstallmentCount || *params.InstallmentNumber < 1 || *params.InstallmentNumber > *params.InstallmentCount {
			return CardStatementLine{}, ErrInvalidCardStatementInstallment
		}
		line.installmentNumber = *params.InstallmentNumber
		line.hasInstallmentNum = true
		line.installmentCount = *params.InstallmentCount
		line.hasInstallmentCnt = true
	default:
		return CardStatementLine{}, ErrInvalidCardStatementPurchaseMode
	}
	return line, nil
}

func (line CardStatementLine) ExpenseID() string                       { return line.expenseID }
func (line CardStatementLine) Description() string                     { return line.description }
func (line CardStatementLine) Amount() Money                           { return line.amount }
func (line CardStatementLine) OccurredAt() time.Time                   { return line.occurredAt }
func (line CardStatementLine) PurchaseMode() CardStatementPurchaseMode { return line.purchaseMode }

func (line CardStatementLine) InstallmentNumber() (int, bool) {
	return line.installmentNumber, line.hasInstallmentNum
}

func (line CardStatementLine) InstallmentCount() (int, bool) {
	return line.installmentCount, line.hasInstallmentCnt
}

func (line CardStatementLine) structurallyValid() bool {
	params := CardStatementLineParams{
		ExpenseID: line.expenseID, Description: line.description, Amount: line.amount,
		OccurredAt: line.occurredAt, PurchaseMode: line.purchaseMode,
	}
	if line.hasInstallmentNum {
		number := line.installmentNumber
		params.InstallmentNumber = &number
	}
	if line.hasInstallmentCnt {
		count := line.installmentCount
		params.InstallmentCount = &count
	}
	canonical, err := NewCardStatementLine(params)
	return err == nil && canonical == line
}

// CardStatementParams defines one requested statement cycle. TotalAmount is
// validated against the exact sum of Lines; no floating point arithmetic is
// involved.
type CardStatementParams struct {
	CreditCardID   string
	StatementDueOn CivilDate
	TotalAmount    Money
	Lines          []CardStatementLine
}

// CardStatement is a read-only projection for one card and one explicit due
// date. It is not persisted and has no payment or settlement state.
type CardStatement struct {
	creditCardID   string
	statementDueOn CivilDate
	totalAmount    Money
	lines          []CardStatementLine
}

func NewCardStatement(params CardStatementParams) (CardStatement, error) {
	if ValidateCreditCardID(params.CreditCardID) != nil {
		return CardStatement{}, ErrInvalidCardStatementCreditCardID
	}
	if !params.StatementDueOn.valid() {
		return CardStatement{}, ErrInvalidCardStatementDueDate
	}
	if params.TotalAmount.Currency() != CurrencyBRL || params.TotalAmount.MinorUnits() < 0 {
		return CardStatement{}, ErrInvalidCardStatementAmount
	}

	lines := append([]CardStatementLine(nil), params.Lines...)
	if lines == nil {
		lines = []CardStatementLine{}
	}
	seen := make(map[cardStatementLineKey]struct{}, len(lines))
	seenExpenseModes := make(map[string]bool, len(lines))
	var total int64
	for _, line := range lines {
		if !line.structurallyValid() {
			return CardStatement{}, ErrInvalidCardStatementLineAmount
		}
		number, hasNumber := line.InstallmentNumber()
		key := cardStatementLineKey{expenseID: line.ExpenseID(), installmentNumber: number, hasInstallment: hasNumber}
		if _, exists := seen[key]; exists {
			return CardStatement{}, ErrCardStatementDuplicateLine
		}
		if previousMode, exists := seenExpenseModes[line.ExpenseID()]; exists && previousMode != hasNumber {
			return CardStatement{}, ErrCardStatementDuplicateLine
		}
		seen[key] = struct{}{}
		seenExpenseModes[line.ExpenseID()] = hasNumber
		amount := line.Amount().MinorUnits()
		if amount > math.MaxInt64-total {
			return CardStatement{}, ErrCardStatementAmountOverflow
		}
		total += amount
	}
	if params.TotalAmount.MinorUnits() != total {
		return CardStatement{}, ErrCardStatementTotalMismatch
	}

	sort.SliceStable(lines, func(left, right int) bool {
		return cardStatementLineComesBefore(lines[left], lines[right])
	})
	return CardStatement{
		creditCardID: params.CreditCardID, statementDueOn: params.StatementDueOn,
		totalAmount: params.TotalAmount, lines: lines,
	}, nil
}

func (statement CardStatement) CreditCardID() string      { return statement.creditCardID }
func (statement CardStatement) StatementDueOn() CivilDate { return statement.statementDueOn }
func (statement CardStatement) TotalAmount() Money        { return statement.totalAmount }

// Lines returns a fresh slice so callers cannot mutate the statement's
// internal collection or change a previously derived result.
func (statement CardStatement) Lines() []CardStatementLine {
	lines := make([]CardStatementLine, len(statement.lines))
	copy(lines, statement.lines)
	return lines
}

type cardStatementLineKey struct {
	expenseID         string
	installmentNumber int
	hasInstallment    bool
}

func cardStatementLineComesBefore(left, right CardStatementLine) bool {
	if !left.OccurredAt().Equal(right.OccurredAt()) {
		return left.OccurredAt().Before(right.OccurredAt())
	}
	if left.ExpenseID() != right.ExpenseID() {
		return left.ExpenseID() < right.ExpenseID()
	}
	leftNumber, leftHas := left.InstallmentNumber()
	rightNumber, rightHas := right.InstallmentNumber()
	if leftHas != rightHas {
		return !leftHas
	}
	if leftNumber != rightNumber {
		return leftNumber < rightNumber
	}
	return left.Amount().MinorUnits() < right.Amount().MinorUnits()
}
