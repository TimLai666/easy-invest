package ledger

// 批次純計算引擎。
//
// 這個檔案只做記憶體內的批次數學，不做任何網路、資料庫或檔案 I/O；
// 批次的載入與寫回由 service 層負責。規則真源：
//   - docs/tw-market-rules.md（費稅、公司行動、FIFO、雙視角成本）
//   - docs/db-schema.md（lots 與 ledger_events 不變式）
//
// 雙視角成本：
//   - original_cost：原始成本（買進價金 + 手續費）。批次建立後不再變動，
//     已實現損益一律以這個視角計算。
//   - adjusted_cost：調整後成本（券商 App 視角）。現金股利、減資退現與
//     成本調整事件會改變它；original_cost 永遠不動。
//   - 兩者都以「批次原始股數 original_quantity」為基準，未平倉部位的有效成本
//     = cost × remaining_quantity / original_quantity（portfolio 查詢同此公式）。

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// lotState 是批次在記憶體中的可變狀態。引擎函式直接修改欄位並把 Dirty 設為 true，
// service 層只把 Dirty 的批次寫回資料庫。
type lotState struct {
	ID                string // 資料庫批次 id；純計算測試可留空
	OpenedByEventID   string
	OpenDate          string
	OriginalQuantity  decimal.Decimal
	RemainingQuantity decimal.Decimal
	OriginalCost      decimal.Decimal
	AdjustedCost      decimal.Decimal
	Dirty             bool
}

// lotConsumption 是一次沖銷在單一批次上的明細，對應 lot_consumptions 一列。
type lotConsumption struct {
	LotID        string
	Quantity     decimal.Decimal
	CostConsumed decimal.Decimal
	RealizedPnL  decimal.Decimal
}

// consumeLots 依 FIFO 順序（呼叫端必須先把 lots 依開倉順序排好）沖銷 quantity 股，
// 每消耗一個批次呼叫一次 fill 取得成本與損益的記法。
// 股數不足時回傳 ErrInsufficientShares；呼叫端應在交易（transaction）內使用，
// 失敗時整筆 rollback，因此部分批次已被修改也不影響資料庫狀態。
func consumeLots(lots []*lotState, quantity decimal.Decimal, fill func(lot *lotState, consume decimal.Decimal) (costConsumed, realized decimal.Decimal)) ([]lotConsumption, error) {
	if quantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("%w: 沖銷股數必須為正數", ErrValidation)
	}
	remaining := quantity
	var consumptions []lotConsumption
	for _, lot := range lots {
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}
		if lot.RemainingQuantity.LessThanOrEqual(decimal.Zero) {
			continue
		}
		consume := decimal.Min(remaining, lot.RemainingQuantity)
		costConsumed, realized := fill(lot, consume)
		consumptions = append(consumptions, lotConsumption{
			LotID:        lot.ID,
			Quantity:     consume,
			CostConsumed: costConsumed,
			RealizedPnL:  realized,
		})
		lot.RemainingQuantity = lot.RemainingQuantity.Sub(consume)
		lot.Dirty = true
		remaining = remaining.Sub(consume)
	}
	if remaining.GreaterThan(decimal.Zero) {
		return nil, ErrInsufficientShares
	}
	return consumptions, nil
}

// consumeLotsFIFO 處理賣出與減資的 FIFO 沖銷。
// netProceeds = 價金 − 手續費 − 證交稅（減資則為退還淨現金），
// 已實現損益 = 淨收入按沖銷股數比例分攤 − 沖銷批次的原始成本
//（docs/tw-market-rules.md「已實現損益」段）。
func consumeLotsFIFO(lots []*lotState, quantity, netProceeds decimal.Decimal) ([]lotConsumption, error) {
	return consumeLots(lots, quantity, func(lot *lotState, consume decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
		costConsumed := lot.OriginalCost.Mul(consume).Div(lot.OriginalQuantity)
		proceedsPart := netProceeds.Mul(consume).Div(quantity)
		return costConsumed, proceedsPart.Sub(costConsumed)
	})
}

// consumeLotsForAdjustment 處理調整事件的負股數沖銷。
// realized_pnl 一律記 0（調整不認列損益）；cost_consumed 預設記 FIFO 比例原始成本，
// 若給定 unitCost 則改記 unitCost × 沖銷股數（語意見 types.go metadata 常數說明）。
func consumeLotsForAdjustment(lots []*lotState, quantity decimal.Decimal, unitCost *decimal.Decimal) ([]lotConsumption, error) {
	return consumeLots(lots, quantity, func(lot *lotState, consume decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
		costConsumed := lot.OriginalCost.Mul(consume).Div(lot.OriginalQuantity)
		if unitCost != nil {
			costConsumed = unitCost.Mul(consume)
		}
		return costConsumed, decimal.Zero
	})
}

// applySplitToLots 股票分割／反分割：
// 所有未平倉批次的 original_quantity 與 remaining_quantity 乘上 ratio（新股數/舊股數），
// 成本欄位不動，每股成本因此等比下降、總成本不變
//（有效成本 cost × remaining/original 在等比縮放下不變）。
// 已平倉批次不動：歷史沖銷紀錄以分割前的股數單位記載，縮放會破壞歷史對帳。
// 回傳淨增股數（反分割為負），僅供事件欄位顯示用。
func applySplitToLots(lots []*lotState, ratio decimal.Decimal) (decimal.Decimal, error) {
	if ratio.LessThanOrEqual(decimal.Zero) || ratio.Equal(decimal.NewFromInt(1)) {
		return decimal.Zero, fmt.Errorf("%w: split_ratio 必須為正數且不等於 1", ErrValidation)
	}
	netNew := decimal.Zero
	for _, lot := range lots {
		if lot.RemainingQuantity.LessThanOrEqual(decimal.Zero) {
			continue
		}
		before := lot.RemainingQuantity
		lot.OriginalQuantity = lot.OriginalQuantity.Mul(ratio)
		lot.RemainingQuantity = lot.RemainingQuantity.Mul(ratio)
		lot.Dirty = true
		netNew = netNew.Add(lot.RemainingQuantity.Sub(before))
	}
	return netNew, nil
}

// deductAdjustedCostProRata 把 amount（正=扣減調整後成本）按未平倉股數比例
// 分攤到各批次的 adjusted_cost；original_cost 不動。
//
// 基準換算：portfolio 視角的有效調整後成本 = adjusted_cost × remaining/original。
// 要讓某批次的有效成本下降 perShare × remaining，adjusted_cost 必須下降
// perShare × original（兩邊同除 remaining/original）。
//
// adjusted_cost 允許變成負數：長期領息可能扣到低於零，券商 App 同樣會顯示負成本。
// 沒有未平倉股數時不動作（例如除息時持股已全數出清）。
func deductAdjustedCostProRata(lots []*lotState, amount decimal.Decimal) {
	if amount.IsZero() {
		return
	}
	total := sumRemainingShares(lots)
	if total.LessThanOrEqual(decimal.Zero) {
		return
	}
	perShare := amount.Div(total)
	for _, lot := range lots {
		if lot.RemainingQuantity.LessThanOrEqual(decimal.Zero) {
			continue
		}
		lot.AdjustedCost = lot.AdjustedCost.Sub(perShare.Mul(lot.OriginalQuantity))
		lot.Dirty = true
	}
}

// sumRemainingShares 回傳未平倉股數合計。
func sumRemainingShares(lots []*lotState) decimal.Decimal {
	total := decimal.Zero
	for _, lot := range lots {
		if lot.RemainingQuantity.GreaterThan(decimal.Zero) {
			total = total.Add(lot.RemainingQuantity)
		}
	}
	return total
}

// metadataDecimal 從事件 metadata 取出十進位數字。
// 接受 JSON 字串與數字兩種寫法；建議一律用字串避免浮點誤差。
// 鍵不存在時回 ok=false 且無錯誤；存在但格式不合法時回 ErrValidation。
func metadataDecimal(metadata map[string]any, key string) (decimal.Decimal, bool, error) {
	if metadata == nil {
		return decimal.Zero, false, nil
	}
	raw, exists := metadata[key]
	if !exists || raw == nil {
		return decimal.Zero, false, nil
	}
	switch v := raw.(type) {
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Zero, false, fmt.Errorf("%w: metadata.%s 不是合法數字: %v", ErrValidation, key, err)
		}
		return d, true, nil
	case float64:
		return decimal.NewFromFloat(v), true, nil
	case int:
		return decimal.NewFromInt(int64(v)), true, nil
	case int64:
		return decimal.NewFromInt(v), true, nil
	default:
		return decimal.Zero, false, fmt.Errorf("%w: metadata.%s 必須是字串或數字", ErrValidation, key)
	}
}
