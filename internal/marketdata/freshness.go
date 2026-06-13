package marketdata

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoMarketData 表示該市場還沒有任何已入庫的日終行情。
var ErrNoMarketData = errors.New("尚無任何市場日終資料")

// StaleTradingDays 回傳某市場日終行情的落後狀態，給建議引擎當資料新鮮度防線：
//   - latest：已入庫（最新版）行情的最後一個資料日。
//   - lagDays：latest 之後到最近一個開市日為止，缺少資料的開市日數；0 表示已是最新。
//
// market 是資產市場（'TWSE' 或 'TPEX'）；台灣市場共用 TWSE 交易日曆。
// 完全沒有資料時回傳 ErrNoMarketData。
func (s *Service) StaleTradingDays(ctx context.Context, market string) (lagDays int, latest time.Time, err error) {
	var latestPtr *time.Time
	err = s.db.QueryRow(ctx, `
		SELECT max(b.bar_date)
		FROM market_daily_bars b
		JOIN assets a ON a.id = b.asset_id
		WHERE b.is_latest AND a.market = $1
	`, market).Scan(&latestPtr)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, err
	}
	if latestPtr == nil {
		return 0, time.Time{}, ErrNoMarketData
	}
	latest = *latestPtr

	today := civilDateIn(time.Now(), taipeiLocation())
	lastOpen, err := s.LastOpenTradingDay(ctx, "TWSE", today)
	if errors.Is(err, pgx.ErrNoRows) {
		// 還沒有交易日曆時無法計算落後天數，誠實回報 0 並由日曆匯入流程補上。
		return 0, latest, nil
	}
	if err != nil {
		return 0, latest, err
	}
	lag, err := s.countOpenDaysBetween(ctx, "TWSE", latest, lastOpen)
	if err != nil {
		return 0, latest, err
	}
	return lag, latest, nil
}

// countOpenDaysBetween 計算 (after, until] 區間內的開市日數。
func (s *Service) countOpenDaysBetween(ctx context.Context, market string, after, until time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM trading_calendar
		WHERE market = $1 AND is_open AND cal_date > $2::date AND cal_date <= $3::date
	`, market, after.Format("2006-01-02"), until.Format("2006-01-02")).Scan(&count)
	return count, err
}

// Freshness 回傳每個 dataset 的資料新鮮度：
// 最近一次匯入的來源與狀態，加上最新資料日（latest_data_date）與
// 落後開市日數（stale_trading_days，null 表示無法計算）。
func (s *Service) Freshness(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		SELECT dataset,
		       max(data_time)::text AS latest_data_time,
		       max(fetched_at)::text AS latest_fetched_at,
		       (array_agg(source_name ORDER BY fetched_at DESC))[1] AS source_name,
		       (array_agg(status ORDER BY fetched_at DESC))[1] AS status,
		       max(data_time) FILTER (WHERE status = 'succeeded')::date AS latest_data_date
		FROM ingestion_runs
		GROUP BY dataset
		ORDER BY dataset
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type freshnessRow struct {
		dataset, dataTime, fetchedAt, source, status string
		latestDataDate                               *time.Time
	}
	var parsed []freshnessRow
	for rows.Next() {
		var item freshnessRow
		var dataTime, fetchedAt *string
		if err := rows.Scan(&item.dataset, &dataTime, &fetchedAt, &item.source, &item.status, &item.latestDataDate); err != nil {
			return nil, err
		}
		if dataTime != nil {
			item.dataTime = *dataTime
		}
		if fetchedAt != nil {
			item.fetchedAt = *fetchedAt
		}
		parsed = append(parsed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 行情類 dataset 的最新資料日以 market_daily_bars 實際入庫為準，比 ingestion_runs 更可信。
	var latestBar *time.Time
	if err := s.db.QueryRow(ctx, `SELECT max(bar_date) FROM market_daily_bars WHERE is_latest`).Scan(&latestBar); err != nil {
		return nil, err
	}

	today := civilDateIn(time.Now(), taipeiLocation())
	lastOpen, lastOpenErr := s.LastOpenTradingDay(ctx, "TWSE", today)

	items := make([]map[string]any, 0, len(parsed))
	for _, row := range parsed {
		latestDate := row.latestDataDate
		if (row.dataset == "daily_bars" || row.dataset == "daily_bars_backfill") && latestBar != nil {
			latestDate = latestBar
		}
		var latestDateText any
		var staleDays any
		if latestDate != nil {
			latestDateText = latestDate.Format("2006-01-02")
			if lastOpenErr == nil {
				if lag, err := s.countOpenDaysBetween(ctx, "TWSE", *latestDate, lastOpen); err == nil {
					staleDays = lag
				}
			}
		}
		items = append(items, map[string]any{
			"dataset":            row.dataset,
			"latest_data_time":   row.dataTime,
			"latest_fetched_at":  row.fetchedAt,
			"source_name":        row.source,
			"status":             row.status,
			"latest_data_date":   latestDateText,
			"stale_trading_days": staleDays,
		})
	}
	return items, nil
}
