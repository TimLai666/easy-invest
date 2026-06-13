package marketdata

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseTPExStockDayRowsConvertsThousands(t *testing.T) {
	rows := [][]string{
		{"113/01/02", "22", "446", "20.00", "20.00", "19.77", "19.77", "-0.23", "37"},
	}
	bars := parseTPExStockDayRows(rows, "006201", "TPEX")
	if len(bars) != 1 {
		t.Fatalf("len = %d, want 1", len(bars))
	}
	got := bars[0]
	if got.Date.Format("2006-01-02") != "2024-01-02" {
		t.Fatalf("date = %s, want 2024-01-02", got.Date.Format("2006-01-02"))
	}
	if !got.Close.Equal(mustDecimal("19.77")) {
		t.Fatalf("close = %s, want 19.77", got.Close)
	}
	if !got.Volume.Equal(mustDecimal("22000")) {
		t.Fatalf("volume = %s, want 22000", got.Volume)
	}
	if !got.Turnover.Equal(mustDecimal("446000")) {
		t.Fatalf("turnover = %s, want 446000", got.Turnover)
	}
}

func TestParseTPExStockDayRowsSkipsNoCloseRows(t *testing.T) {
	rows := [][]string{
		{"113/01/02", "0", "0", "", "", "", "", "", "0"},
	}
	if got := parseTPExStockDayRows(rows, "006201", "TPEX"); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func mustDecimal(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}
