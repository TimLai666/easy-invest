package ledger

import "github.com/shopspring/decimal"

const (
	EventBuy                            = "buy"
	EventSell                           = "sell"
	EventCashDividend                   = "cash_dividend"
	EventStockDividend                  = "stock_dividend"
	EventSplit                          = "split"
	EventCapitalReduction               = "capital_reduction"
	EventCashDeposit                    = "cash_deposit"
	EventCashWithdraw                   = "cash_withdraw"
	EventFeeAdjustment                  = "fee_adjustment"
	EventTaxAdjustment                  = "tax_adjustment"
	EventBrokerReconciliationAdjustment = "broker_reconciliation_adjustment"
	EventManualCorrection               = "manual_correction"
)

type CreateEventInput struct {
	UserID         string
	EventType      string
	Symbol         string
	TradeDate      string
	SettlementDate *string
	Quantity       string
	Unit           string
	Price          string
	GrossAmount    *string
	Fee            *string
	Tax            *string
	Source         string
	SourceRef      *string
	Notes          string
	Metadata       map[string]any
}

type Event struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id,omitempty"`
	AssetID        *string        `json:"asset_id,omitempty"`
	Symbol         *string        `json:"symbol,omitempty"`
	EventType      string         `json:"event_type"`
	TradeDate      string         `json:"trade_date"`
	SettlementDate *string        `json:"settlement_date,omitempty"`
	QuantityShares *string        `json:"quantity_shares,omitempty"`
	QuantityLots   *string        `json:"quantity_lots,omitempty"`
	Price          *string        `json:"price,omitempty"`
	GrossAmount    *string        `json:"gross_amount,omitempty"`
	Fee            string         `json:"fee"`
	Tax            string         `json:"tax"`
	CashDelta      string         `json:"cash_delta"`
	Currency       string         `json:"currency"`
	FeeSource      string         `json:"fee_source"`
	Source         string         `json:"source"`
	SourceRef      *string        `json:"source_ref,omitempty"`
	Notes          string         `json:"notes"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at"`
	VoidedAt       *string        `json:"voided_at,omitempty"`
	VoidReason     *string        `json:"void_reason,omitempty"`
}

type Lot struct {
	ID                string  `json:"id"`
	AssetID           string  `json:"asset_id"`
	Symbol            string  `json:"symbol"`
	OpenedByEventID   string  `json:"opened_by_event_id"`
	OpenDate          string  `json:"open_date"`
	OriginalQuantity  string  `json:"original_quantity"`
	RemainingQuantity string  `json:"remaining_quantity"`
	OriginalCost      string  `json:"original_cost"`
	AdjustedCost      string  `json:"adjusted_cost"`
	ClosedAt          *string `json:"closed_at,omitempty"`
}

type Position struct {
	AssetID             string  `json:"asset_id"`
	Symbol              string  `json:"symbol"`
	Name                string  `json:"name"`
	AssetType           string  `json:"asset_type"`
	Market              string  `json:"market"`
	Currency            string  `json:"currency"`
	LotSize             int     `json:"lot_size"`
	QuantityShares      string  `json:"quantity_shares"`
	QuantityLots        string  `json:"quantity_lots"`
	OriginalCost        string  `json:"original_cost"`
	AdjustedCost        string  `json:"adjusted_cost"`
	AverageCost         string  `json:"average_cost"`
	AdjustedAverageCost string  `json:"adjusted_average_cost"`
	MarketPrice         *string `json:"market_price,omitempty"`
	MarketValue         *string `json:"market_value,omitempty"`
	UnrealizedPnL       *string `json:"unrealized_pnl,omitempty"`
	MarketDataAsOf      *string `json:"market_data_as_of,omitempty"`
}

type Portfolio struct {
	UserID         string     `json:"user_id"`
	AsOf           string     `json:"as_of"`
	Cash           string     `json:"cash"`
	Positions      []Position `json:"positions"`
	MarketDataAsOf *string    `json:"market_data_as_of,omitempty"`
}

type settingsRow struct {
	FeeRate             decimal.Decimal
	FeeDiscount         decimal.Decimal
	FeeMinimum          decimal.Decimal
	DividendTransferFee decimal.Decimal
}
