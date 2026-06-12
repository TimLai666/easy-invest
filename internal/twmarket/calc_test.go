package twmarket

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestAcceptanceExamples(t *testing.T) {
	settings := DefaultFeeSettings()
	settings.FeeDiscount = decimal.RequireFromString("0.6")

	t.Run("buy one lot 2330", func(t *testing.T) {
		shares, err := Shares(decimal.NewFromInt(1), UnitLot, 1000)
		if err != nil {
			t.Fatal(err)
		}
		gross := GrossAmount(shares, decimal.NewFromInt(1000))
		fee := EstimateFee(gross, settings)
		cost := gross.Add(fee)
		if got, want := gross.String(), "1000000"; got != want {
			t.Fatalf("gross = %s, want %s", got, want)
		}
		if got, want := fee.String(), "855"; got != want {
			t.Fatalf("fee = %s, want %s", got, want)
		}
		if got, want := cost.String(), "1000855"; got != want {
			t.Fatalf("cost = %s, want %s", got, want)
		}
	})

	t.Run("odd lot buy applies minimum fee", func(t *testing.T) {
		gross := GrossAmount(decimal.NewFromInt(50), decimal.NewFromInt(180))
		fee := EstimateFee(gross, settings)
		if got, want := gross.String(), "9000"; got != want {
			t.Fatalf("gross = %s, want %s", got, want)
		}
		if got, want := fee.String(), "20"; got != want {
			t.Fatalf("fee = %s, want %s", got, want)
		}
		if got, want := gross.Add(fee).String(), "9020"; got != want {
			t.Fatalf("cost = %s, want %s", got, want)
		}
	})

	t.Run("sell stock with FIFO cost", func(t *testing.T) {
		gross := GrossAmount(decimal.NewFromInt(500), decimal.NewFromInt(1100))
		fee := EstimateFee(gross, settings)
		tax := EstimateSecuritiesTax(gross, AssetTWStock)
		cost := decimal.RequireFromString("1000855").Mul(decimal.RequireFromString("0.5"))
		pnl := gross.Sub(fee).Sub(tax).Sub(cost)
		if got, want := fee.String(), "470"; got != want {
			t.Fatalf("fee = %s, want %s", got, want)
		}
		if got, want := tax.String(), "1650"; got != want {
			t.Fatalf("tax = %s, want %s", got, want)
		}
		if got, want := pnl.String(), "47452.5"; got != want {
			t.Fatalf("pnl = %s, want %s", got, want)
		}
	})

	t.Run("cash dividend", func(t *testing.T) {
		gross := decimal.NewFromInt(2000).Mul(decimal.NewFromInt(12))
		fee, tax, net := CashDividendNet(gross, settings)
		if got, want := fee.String(), "10"; got != want {
			t.Fatalf("fee = %s, want %s", got, want)
		}
		if got, want := tax.String(), "506"; got != want {
			t.Fatalf("tax = %s, want %s", got, want)
		}
		if got, want := net.String(), "23484"; got != want {
			t.Fatalf("net = %s, want %s", got, want)
		}
	})

	t.Run("stock dividend", func(t *testing.T) {
		newShares := StockDividendShares(decimal.NewFromInt(2000), decimal.RequireFromString("1.5"))
		if got, want := newShares.String(), "300"; got != want {
			t.Fatalf("new shares = %s, want %s", got, want)
		}
	})
}
