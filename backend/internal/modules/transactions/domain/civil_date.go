package domain

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidCivilDate = errors.New("civil date: invalid value")

// CivilDate represents a Gregorian calendar date without a timezone or a
// time-of-day. Its zero value is invalid.
type CivilDate struct {
	year  int
	month time.Month
	day   int
}

// NewCivilDate validates and constructs a date using only calendar fields.
func NewCivilDate(year int, month time.Month, day int) (CivilDate, error) {
	date := CivilDate{year: year, month: month, day: day}
	if !date.valid() {
		return CivilDate{}, ErrInvalidCivilDate
	}
	return date, nil
}

func (date CivilDate) Year() int         { return date.year }
func (date CivilDate) Month() time.Month { return date.month }
func (date CivilDate) Day() int          { return date.day }

// String returns the canonical transport-ready calendar representation.
func (date CivilDate) String() string {
	if !date.valid() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", date.year, date.month, date.day)
}

func (date CivilDate) Equal(other CivilDate) bool {
	return date == other
}

func (date CivilDate) Before(other CivilDate) bool {
	if date.year != other.year {
		return date.year < other.year
	}
	if date.month != other.month {
		return date.month < other.month
	}
	return date.day < other.day
}

func (date CivilDate) valid() bool {
	if date.year < 1 || date.year > 9999 || date.month < time.January || date.month > time.December {
		return false
	}
	return date.day >= 1 && date.day <= daysInMonth(date.year, date.month)
}

func daysInMonth(year int, month time.Month) int {
	switch month {
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if isLeapYear(year) {
			return 29
		}
		return 28
	default:
		return 31
	}
}

func isLeapYear(year int) bool {
	return year%400 == 0 || year%4 == 0 && year%100 != 0
}
