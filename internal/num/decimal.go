package num

import (
	"database/sql"

	"github.com/shopspring/decimal"
)

func Parse(value string) decimal.Decimal {
	if value == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func PtrString(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	value := d.String()
	return &value
}

func String(d decimal.Decimal) string {
	return d.String()
}

func NullDecimal(ns sql.NullString) decimal.Decimal {
	if !ns.Valid {
		return decimal.Zero
	}
	return Parse(ns.String)
}

func DecimalPtr(ns sql.NullString) *decimal.Decimal {
	if !ns.Valid {
		return nil
	}
	d := Parse(ns.String)
	return &d
}
