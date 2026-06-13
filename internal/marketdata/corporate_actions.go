package marketdata

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// twt48uRow 是 TWSE OpenAPI TWT48U_ALL（上市股票除權除息預告表）的單列格式。
// 實測（2026-06-12）：原規劃的 TWT49U 在 OpenAPI 上不存在（302→404），改用 TWT48U_ALL。
// Date 是除權息日（民國年緊湊格式）；Exdividend 為「息」「權」「權息」。
type twt48uRow struct {
	Date               string `json:"Date"`
	Code               string `json:"Code"`
	Name               string `json:"Name"`
	Exdividend         string `json:"Exdividend"`
	CashDividend       string `json:"CashDividend"`
	StockDividendRatio string `json:"StockDividendRatio"`
}

// corporateActionRow 是解析後待入庫的公司行動。
type corporateActionRow struct {
	Symbol       string
	Name         string
	ActionType   string // cash_dividend / stock_dividend
	ExDate       time.Time
	CashPerShare decimal.Decimal
	StockRatio   decimal.Decimal
	Raw          twt48uRow
}

// ImportTWSECorporateActions 匯入上市除權除息預告（TWT48U_ALL）到 corporate_actions。
// 同一（標的, 行動類型, 除權息日）已存在時更新數值（來源修正預告金額屬常態），
// 不影響 ledger——配息入帳是使用者層的 ledger 事件，公司行動表只是市場事實。
func (p *Pipeline) ImportTWSECorporateActions(ctx context.Context) (ImportResult, error) {
	runID, err := p.beginIngestionRun(ctx, "twse_openapi", p.cfg.TWSECorpActionsURL, "TWSE OpenAPI", "corporate_actions", nil)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{IngestionRunID: runID, Status: "running"}

	var rows []twt48uRow
	checksum, err := p.fetchJSON(ctx, p.twseLimiter, p.cfg.TWSECorpActionsURL, &rows)
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}

	actions := parseTWT48URows(rows)
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}
	defer tx.Rollback(ctx)

	imported := 0
	for _, action := range actions {
		assetID, err := upsertAsset(ctx, tx, "TWSE", action.Symbol, action.Name)
		if err != nil {
			result.Rows = imported
			return p.failImport(ctx, result, checksum, err)
		}
		details, _ := json.Marshal(map[string]any{
			"source":      "twse_twt48u_all",
			"exdividend":  action.Raw.Exdividend,
			"raw_cash":    action.Raw.CashDividend,
			"raw_ratio":   action.Raw.StockDividendRatio,
			"announced":   true,
			"is_forecast": true, // 預告表資料，金額可能於除權息日前修正
		})
		var cash, ratio *string
		if action.ActionType == "cash_dividend" {
			v := action.CashPerShare.String()
			cash = &v
		} else {
			v := action.StockRatio.String()
			ratio = &v
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO corporate_actions (asset_id, action_type, ex_date, cash_per_share, stock_ratio, details, ingestion_run_id)
			VALUES ($1, $2, $3::date, $4::numeric, $5::numeric, $6, $7)
			ON CONFLICT (asset_id, action_type, ex_date)
			DO UPDATE SET cash_per_share = EXCLUDED.cash_per_share,
			              stock_ratio = EXCLUDED.stock_ratio,
			              details = EXCLUDED.details,
			              ingestion_run_id = EXCLUDED.ingestion_run_id
		`, assetID, action.ActionType, action.ExDate.Format("2006-01-02"), cash, ratio, details, runID); err != nil {
			result.Rows = imported
			return p.failImport(ctx, result, checksum, err)
		}
		imported++
	}
	if err := tx.Commit(ctx); err != nil {
		result.Rows = imported
		return p.failImport(ctx, result, checksum, err)
	}
	result.Rows = imported
	if err := p.finishIngestionRun(ctx, runID, "succeeded", imported, checksum, "", nil); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	return result, nil
}

// parseTWT48URows 把除權息預告列拆成現金股利與股票股利兩種公司行動（純函式，可單元測試）。
// 「權息」會拆成兩筆；CashDividend / StockDividendRatio 空白或為零的部分略過
// （預告表常先公告除權息日、金額後補）。
func parseTWT48URows(rows []twt48uRow) []corporateActionRow {
	var actions []corporateActionRow
	for _, row := range rows {
		symbol := strings.TrimSpace(row.Code)
		if symbol == "" {
			continue
		}
		exDate, err := ParseROCCompactDate(row.Date)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if cash, ok := parseTWNumber(row.CashDividend); ok && cash.GreaterThan(decimal.Zero) {
			actions = append(actions, corporateActionRow{
				Symbol: symbol, Name: name, ActionType: "cash_dividend",
				ExDate: exDate, CashPerShare: cash, Raw: row,
			})
		}
		if ratio, ok := parseTWNumber(row.StockDividendRatio); ok && ratio.GreaterThan(decimal.Zero) {
			actions = append(actions, corporateActionRow{
				Symbol: symbol, Name: name, ActionType: "stock_dividend",
				ExDate: exDate, StockRatio: ratio, Raw: row,
			})
		}
	}
	return actions
}
