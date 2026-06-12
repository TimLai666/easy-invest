package twmarket

import (
	"fmt"

	"github.com/shopspring/decimal"
)

const (
	UnitShare = "share"
	UnitLot   = "lot"

	AssetTWStock   = "tw_stock"
	AssetTWETF     = "tw_etf"
	AssetTWBondETF = "tw_bond_etf"
)

var (
	DefaultFeeRate     = decimal.RequireFromString("0.001425")
	DefaultFeeDiscount = decimal.NewFromInt(1)
	DefaultFeeMinimum  = decimal.NewFromInt(20)

	StockTaxRate         = decimal.RequireFromString("0.003")
	ETFTaxRate           = decimal.RequireFromString("0.001")
	DividendPremiumRate  = decimal.RequireFromString("0.0211")
	DividendPremiumFloor = decimal.NewFromInt(20000)
	DefaultDividendFee   = decimal.NewFromInt(10)
	DefaultTaiwanLotSize = decimal.NewFromInt(1000)
	Zero                 = decimal.Zero
	One                  = decimal.NewFromInt(1)
)

type FeeSettings struct {
	FeeRate             decimal.Decimal
	FeeDiscount         decimal.Decimal
	FeeMinimum          decimal.Decimal
	DividendTransferFee decimal.Decimal
}

func DefaultFeeSettings() FeeSettings {
	return FeeSettings{
		FeeRate:             DefaultFeeRate,
		FeeDiscount:         DefaultFeeDiscount,
		FeeMinimum:          DefaultFeeMinimum,
		DividendTransferFee: DefaultDividendFee,
	}
}

func Shares(quantity decimal.Decimal, unit string, lotSize int) (decimal.Decimal, error) {
	if quantity.LessThanOrEqual(Zero) {
		return Zero, fmt.Errorf("quantity must be positive")
	}
	if lotSize <= 0 {
		lotSize = 1
	}
	switch unit {
	case UnitShare:
		return quantity, nil
	case UnitLot:
		return quantity.Mul(decimal.NewFromInt(int64(lotSize))), nil
	default:
		return Zero, fmt.Errorf("unit must be share or lot")
	}
}

func Lots(quantityShares decimal.Decimal, lotSize int) decimal.Decimal {
	if lotSize <= 0 {
		lotSize = 1
	}
	return quantityShares.Div(decimal.NewFromInt(int64(lotSize)))
}

func GrossAmount(quantityShares, price decimal.Decimal) decimal.Decimal {
	return quantityShares.Mul(price)
}

func EstimateFee(gross decimal.Decimal, settings FeeSettings) decimal.Decimal {
	rate := settings.FeeRate
	if rate.IsZero() {
		rate = DefaultFeeRate
	}
	discount := settings.FeeDiscount
	if discount.IsZero() {
		discount = DefaultFeeDiscount
	}
	minimum := settings.FeeMinimum
	if minimum.IsNegative() {
		minimum = Zero
	}
	fee := gross.Mul(rate).Mul(discount).Floor()
	if fee.LessThan(minimum) {
		return minimum
	}
	return fee
}

func EstimateSecuritiesTax(gross decimal.Decimal, assetType string) decimal.Decimal {
	rate := Zero
	switch assetType {
	case AssetTWStock:
		rate = StockTaxRate
	case AssetTWETF:
		rate = ETFTaxRate
	case AssetTWBondETF:
		rate = Zero
	default:
		rate = StockTaxRate
	}
	return gross.Mul(rate).Floor()
}

func DividendHealthPremium(gross decimal.Decimal) decimal.Decimal {
	if gross.LessThan(DividendPremiumFloor) {
		return Zero
	}
	return gross.Mul(DividendPremiumRate).Floor()
}

func CashDividendNet(gross decimal.Decimal, settings FeeSettings) (fee decimal.Decimal, tax decimal.Decimal, net decimal.Decimal) {
	fee = settings.DividendTransferFee
	if fee.IsZero() {
		fee = DefaultDividendFee
	}
	tax = DividendHealthPremium(gross)
	net = gross.Sub(fee).Sub(tax)
	return fee, tax, net
}

func StockDividendShares(currentShares, stockDividendPerShareTWD decimal.Decimal) decimal.Decimal {
	ratio := stockDividendPerShareTWD.Div(decimal.NewFromInt(10))
	return currentShares.Mul(ratio).Floor()
}

func Format(d decimal.Decimal) string {
	return d.StringFixedBank(6)
}
