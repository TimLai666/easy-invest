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

// 事件 metadata 慣例鍵。數字一律建議用字串表示（例如 "1234.5"），避免 JSON 浮點誤差；
// 服務層也接受 JSON 數字，會以最短表示法轉成 decimal。
//
// 股票分割事件（split）：
//   - MetaSplitRatio：必填，新股數/舊股數（例如 "2" 表示 1 拆 2、"0.5" 表示 2 併 1）。
//     分割日將該資產所有未平倉批次的 original_quantity 與 remaining_quantity 乘上比率，
//     成本欄位不動（每股成本等比下降、總成本不變）。
//     事件的 quantity_shares 記建立當下的淨增股數（反分割為負），僅供顯示；
//     rebuild 一律以 metadata 的比率重算，不依賴該欄位。
//     反分割產生的畸零股找零請另記一筆現金事件（與配股畸零股同規則）。
//
// 調整事件（fee_adjustment / tax_adjustment / broker_reconciliation_adjustment /
// manual_correction）支援三種作用，可單獨或同時使用：
//
//  1. 純現金調整：現金影響取 MetaCashDelta（有號，正=現金增加）；
//     未提供時退回 gross − fee − tax（例如補收手續費可直接帶 fee=30，現金即 −30）。
//  2. 股數調整：quantity 帶正負號（只有調整事件允許負數）。
//     正數=補股，建立新批次，每股成本取 MetaUnitCost（未提供視為 0，零成本補股），
//     original_cost 與 adjusted_cost 都以該成本起算。
//     負數=依 FIFO 沖銷既有批次，realized_pnl 一律記 0（調整不認列損益）；
//     lot_consumptions.cost_consumed 預設記 FIFO 比例原始成本，
//     若提供 MetaUnitCost 則改記 unit_cost × 沖銷股數。
//  3. 成本調整：MetaAdjustedCostDelta（有號，正=調整後成本增加）按未平倉股數比例
//     分攤到該資產各批次的 adjusted_cost；original_cost 永不變動。
const (
	MetaSplitRatio        = "split_ratio"
	MetaAdjustedCostDelta = "adjusted_cost_delta"
	MetaUnitCost          = "unit_cost"
	MetaCashDelta         = "cash_delta"
)

// IsAdjustmentEvent 回報事件型別是否屬於對帳／修正用的調整事件。
func IsAdjustmentEvent(eventType string) bool {
	switch eventType {
	case EventFeeAdjustment, EventTaxAdjustment, EventBrokerReconciliationAdjustment, EventManualCorrection:
		return true
	}
	return false
}

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
