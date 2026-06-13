package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// stockDayMinDate 是 TWSE 個股日成交資訊（STOCK_DAY）可查詢的最早日期。
// 實測：早於民國 99 年 1 月 4 日會回 stat="查詢日期小於99年1月4日，請重新查詢!"。
var stockDayMinDate = time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)

const (
	datasetDailyBarsBackfill = "daily_bars_backfill"
	benchmarkSymbol          = "0050"
)

// TrackedSymbol 是日終行情追蹤範圍內的一個標的。
type TrackedSymbol struct {
	AssetID            string
	Symbol             string
	Market             string
	EarliestLedgerDate *time.Time
}

// stockDayResponse 是 TWSE STOCK_DAY 的回應格式。
// 實測：stat="OK" 時 data 列為 10 欄
// [日期(民國年斜線), 成交股數, 成交金額, 開盤價, 最高價, 最低價, 收盤價, 漲跌價差, 成交筆數, 註記]，
// 數字帶千分位逗號；無資料或日期超界時 stat 是中文錯誤訊息、total=0。
type stockDayResponse struct {
	Stat   string     `json:"stat"`
	Date   string     `json:"date"`
	Title  string     `json:"title"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

// TrackedSymbols 回傳行情追蹤範圍：出現在交易流水的標的 ∪ 所有使用者的關注清單 ∪ 基準標的（0050）。
// 這是系統層的抓取範圍彙總（決定要向 TWSE 抓哪些標的），不是使用者資料查詢，
// 所以不帶 user_id 條件；任何回傳使用者資料的查詢仍必須帶 user_id。
func (s *Service) TrackedSymbols(ctx context.Context) ([]TrackedSymbol, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.id::text, a.symbol, a.market, min(le.trade_date)
		FROM assets a
		LEFT JOIN ledger_events le ON le.asset_id = a.id
		WHERE a.id IN (
			SELECT asset_id FROM ledger_events WHERE asset_id IS NOT NULL
			UNION
			SELECT asset_id FROM watchlist
		)
		   OR (a.symbol = $1 AND a.market = 'TWSE')
		GROUP BY a.id, a.symbol, a.market
		ORDER BY a.symbol
	`, benchmarkSymbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []TrackedSymbol
	for rows.Next() {
		var item TrackedSymbol
		if err := rows.Scan(&item.AssetID, &item.Symbol, &item.Market, &item.EarliestLedgerDate); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// backfillStartDate 計算某標的的回補起點：
// 持有過的標的從最早交易日再往前推策略歷史窗口；其餘（關注、基準）從今天往前推窗口。
// 起點不早於來源可查詢的最早日期。
func backfillStartDate(sym TrackedSymbol, today time.Time, backfillMonths int) time.Time {
	var start time.Time
	if sym.EarliestLedgerDate != nil {
		start = sym.EarliestLedgerDate.AddDate(0, -backfillMonths, 0)
	} else {
		start = today.AddDate(0, -backfillMonths, 0)
	}
	if start.Before(stockDayMinDate) {
		start = stockDayMinDate
	}
	return start
}

// missingMonths 計算缺口月份（純函式，可單元測試）：
// 「應有交易日集合 − 已入庫日集合」中出現缺日的月份，依時間遞增、每月一筆（該月第一天）。
func missingMonths(requiredDays []time.Time, haveDates map[string]bool) []time.Time {
	var months []time.Time
	seen := make(map[string]bool)
	for _, day := range requiredDays {
		if haveDates[day.Format("2006-01-02")] {
			continue
		}
		month := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
		key := month.Format("2006-01")
		if !seen[key] {
			seen[key] = true
			months = append(months, month)
		}
	}
	return months
}

// shouldFetchMonth 依 checkpoint 狀態決定是否要抓某標的×月份（純函式，可單元測試）：
//   - unrecoverable：永遠跳過（來源確定無資料）。
//   - succeeded：過去月份跳過（缺的日子是來源本來就沒有的，例如停牌）；當月允許重抓。
//   - pending / failed / 無紀錄：抓。
func shouldFetchMonth(status string, month, currentMonth time.Time) bool {
	switch status {
	case "unrecoverable":
		return false
	case "succeeded":
		return !month.Before(currentMonth)
	default:
		return true
	}
}

// existingBarDates 回傳某資產在區間內已入庫（最新版）的日期集合。
func (s *Service) existingBarDates(ctx context.Context, assetID string, from, to time.Time) (map[string]bool, error) {
	rows, err := s.db.Query(ctx, `
		SELECT bar_date::text FROM market_daily_bars
		WHERE asset_id = $1 AND is_latest AND bar_date >= $2::date AND bar_date <= $3::date
	`, assetID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := make(map[string]bool)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		have[date] = true
	}
	return have, rows.Err()
}

// checkpointStatus 讀取 backfill 斷點狀態；沒有紀錄時回傳空字串。
func (p *Pipeline) checkpointStatus(ctx context.Context, dataset, symbol string, month time.Time) (string, error) {
	var status string
	err := p.db.QueryRow(ctx, `
		SELECT status FROM backfill_checkpoints
		WHERE dataset = $1 AND symbol = $2 AND month = $3::date
	`, dataset, symbol, month.Format("2006-01-02")).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return status, err
}

// markCheckpoint 寫入 backfill 斷點結果（attempts 累加）。
// backfill_checkpoints 是抓取進度紀錄，不是 ledger 業務事件，允許 UPDATE。
func (p *Pipeline) markCheckpoint(ctx context.Context, dataset, symbol string, month time.Time, status, lastError string) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO backfill_checkpoints (dataset, symbol, month, status, attempts, last_error)
		VALUES ($1, $2, $3::date, $4, 1, NULLIF($5, ''))
		ON CONFLICT (dataset, symbol, month)
		DO UPDATE SET status = EXCLUDED.status,
		              attempts = backfill_checkpoints.attempts + 1,
		              last_error = EXCLUDED.last_error,
		              updated_at = now()
	`, dataset, symbol, month.Format("2006-01-02"), status, lastError)
	return err
}

// BackfillSymbolMonth 用 TWSE 個股日成交資訊（STOCK_DAY，按月）回補一個標的×月份。
// 回傳入庫列數；來源確定無資料時回傳 errUnrecoverable 包裝的錯誤。
var errUnrecoverable = errors.New("來源無此區間資料，缺口標記為 unrecoverable")

func (p *Pipeline) BackfillSymbolMonth(ctx context.Context, sym TrackedSymbol, month time.Time) (int, error) {
	url := fmt.Sprintf("%s?response=json&date=%s01&stockNo=%s", p.cfg.TWSEStockDayURL, month.Format("200601"), sym.Symbol)
	dataTime := month
	runID, err := p.beginIngestionRun(ctx, "twse", url, "TWSE 公開資料", datasetDailyBarsBackfill, &dataTime)
	if err != nil {
		return 0, err
	}

	var payload stockDayResponse
	checksum, err := p.fetchJSON(ctx, p.twseLimiter, url, &payload)
	if err != nil {
		_ = p.finishIngestionRun(ctx, runID, "failed", 0, "", err.Error(), nil)
		return 0, err
	}
	if !strings.EqualFold(payload.Stat, "OK") {
		// 早於來源支援下限的查詢是永久缺口，不重試。
		if strings.Contains(payload.Stat, "查詢日期小於") {
			msg := fmt.Sprintf("STOCK_DAY 回應: %s", payload.Stat)
			_ = p.finishIngestionRun(ctx, runID, "failed", 0, checksum, msg, nil)
			return 0, fmt.Errorf("%w: %s", errUnrecoverable, msg)
		}
		msg := fmt.Sprintf("STOCK_DAY 回應狀態異常: %s", payload.Stat)
		_ = p.finishIngestionRun(ctx, runID, "failed", 0, checksum, msg, nil)
		return 0, errors.New(msg)
	}
	if len(payload.Data) == 0 {
		// 整月查無資料：過去月份視為來源確定沒有（例如尚未上市），標記永久缺口。
		msg := "STOCK_DAY 該月份無任何資料"
		_ = p.finishIngestionRun(ctx, runID, "succeeded", 0, checksum, "", &dataTime)
		if month.Before(time.Date(p.now().Year(), p.now().Month(), 1, 0, 0, 0, 0, time.UTC)) {
			return 0, fmt.Errorf("%w: %s", errUnrecoverable, msg)
		}
		return 0, nil
	}

	bars := parseStockDayRows(payload.Data, sym.Symbol, sym.Market)
	imported, skipped, err := p.saveDailyBars(ctx, runID, bars)
	if err != nil {
		_ = p.finishIngestionRun(ctx, runID, "failed", imported, checksum, err.Error(), nil)
		return imported, err
	}
	latest := latestBarDate(bars)
	if err := p.finishIngestionRun(ctx, runID, "succeeded", imported, checksum, "", latest); err != nil {
		return imported, err
	}
	p.log.Info("回補完成", "symbol", sym.Symbol, "month", month.Format("2006-01"), "rows", imported, "skipped", skipped)
	return imported, nil
}

// parseStockDayRows 把 STOCK_DAY 的 data 列標準化（純函式，可單元測試）。
// 欄位順序: [日期, 成交股數, 成交金額, 開盤價, 最高價, 最低價, 收盤價, 漲跌價差, 成交筆數, 註記]。
func parseStockDayRows(data [][]string, symbol, market string) []dailyBarRow {
	bars := make([]dailyBarRow, 0, len(data))
	for _, row := range data {
		if len(row) < 7 {
			continue
		}
		barDate, err := ParseROCSlashDate(row[0])
		if err != nil {
			continue
		}
		closePrice, ok := parseTWNumber(row[6])
		if !ok || closePrice.IsZero() {
			// 停牌或無成交日（收盤價 "--"），來源本來就沒有價格，略過。
			continue
		}
		volume, _ := parseTWNumber(row[1])
		turnover, _ := parseTWNumber(row[2])
		open, _ := parseTWNumber(row[3])
		high, _ := parseTWNumber(row[4])
		low, _ := parseTWNumber(row[5])
		bars = append(bars, dailyBarRow{
			Symbol: symbol, Market: market, Date: barDate,
			Open: open, High: high, Low: low, Close: closePrice, Volume: volume, Turnover: turnover,
		})
	}
	return bars
}

// RunCatchUp 是匯入管線的核心語意：把資料補齊到最新，而不是「跑今天」。
// 每次啟動與每個排程週期都做同一件事：
//  1. 確保交易日曆涵蓋回補範圍與當年。
//  2. 匯入當日快照（上市、上櫃）與除權息預告。
//  3. 對每個追蹤標的計算「應有交易日 − 已入庫日」的缺口，逐月序列回補（斷點續傳）。
//
// 冪等：重複執行只會跳過已一致的資料。
func (p *Pipeline) RunCatchUp(ctx context.Context) error {
	today := civilDateIn(p.now(), taipeiLocation())

	symbols, err := p.svc.TrackedSymbols(ctx)
	if err != nil {
		return fmt.Errorf("讀取追蹤標的失敗: %w", err)
	}

	// 1. 日曆：從回補範圍最早年份補到今年（已存在的年份不重抓）。
	startYear := today.Year()
	for _, sym := range symbols {
		y := backfillStartDate(sym, today, p.cfg.BackfillMonths).Year()
		if y < startYear {
			startYear = y
		}
	}
	for year := startYear; year <= today.Year(); year++ {
		if err := p.EnsureCalendarYear(ctx, year); err != nil {
			p.log.Warn("交易日曆匯入失敗", "year", year, "error", err)
			if year == today.Year() {
				return fmt.Errorf("當年度交易日曆無法取得，無法判定缺口: %w", err)
			}
		}
	}

	// 2. 當日快照與公司行動（單一請求，失敗不擋住回補）。
	if result, err := p.ImportTWSEDailyAll(ctx); err != nil {
		p.log.Warn("上市日終快照匯入失敗", "error", err, "run_id", result.IngestionRunID)
	} else {
		p.log.Info("上市日終快照匯入完成", "rows", result.Rows, "skipped", result.Skipped)
	}
	if result, err := p.ImportTPExDailyAll(ctx); err != nil {
		p.log.Warn("上櫃日終快照匯入失敗", "error", err, "run_id", result.IngestionRunID)
	} else {
		p.log.Info("上櫃日終快照匯入完成", "rows", result.Rows, "skipped", result.Skipped)
	}
	if result, err := p.ImportTWSECorporateActions(ctx); err != nil {
		p.log.Warn("除權息預告匯入失敗", "error", err, "run_id", result.IngestionRunID)
	} else {
		p.log.Info("除權息預告匯入完成", "rows", result.Rows)
	}

	// 3. 逐標的回補缺口（序列執行，不平行打同一來源）。
	lastOpen, err := p.svc.LastOpenTradingDay(ctx, "TWSE", today)
	if err != nil {
		return fmt.Errorf("查詢最近開市日失敗: %w", err)
	}
	currentMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)

	for _, sym := range symbols {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p.twseLimiter.Paused() {
			p.log.Warn("TWSE 連續失敗達上限，本輪回補提前結束，等下個排程週期再試")
			return nil
		}
		if sym.Market != "TWSE" {
			// TODO: TPEx 個股歷史回補端點尚未實測（見 docs/data-sources.md），上櫃標的暫時只有當日快照。
			continue
		}
		start := backfillStartDate(sym, today, p.cfg.BackfillMonths)
		required, err := p.svc.TradingDaysBetween(ctx, "TWSE", start, lastOpen)
		if err != nil {
			return err
		}
		have, err := p.svc.existingBarDates(ctx, sym.AssetID, start, lastOpen)
		if err != nil {
			return err
		}
		for _, month := range missingMonths(required, have) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if p.twseLimiter.Paused() {
				p.log.Warn("TWSE 連續失敗達上限，本輪回補提前結束，等下個排程週期再試")
				return nil
			}
			status, err := p.checkpointStatus(ctx, datasetDailyBarsBackfill, sym.Symbol, month)
			if err != nil {
				return err
			}
			if !shouldFetchMonth(status, month, currentMonth) {
				continue
			}
			_, err = p.BackfillSymbolMonth(ctx, sym, month)
			switch {
			case errors.Is(err, errUnrecoverable):
				if cpErr := p.markCheckpoint(ctx, datasetDailyBarsBackfill, sym.Symbol, month, "unrecoverable", err.Error()); cpErr != nil {
					return cpErr
				}
			case err != nil:
				p.log.Warn("回補失敗", "symbol", sym.Symbol, "month", month.Format("2006-01"), "error", err)
				if cpErr := p.markCheckpoint(ctx, datasetDailyBarsBackfill, sym.Symbol, month, "failed", err.Error()); cpErr != nil {
					return cpErr
				}
			default:
				if cpErr := p.markCheckpoint(ctx, datasetDailyBarsBackfill, sym.Symbol, month, "succeeded", ""); cpErr != nil {
					return cpErr
				}
			}
		}
	}
	return nil
}
