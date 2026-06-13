package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// twHolidayResponse 是 TWSE 市場開休市日期查詢（rwd 版）的回應格式。
// 實測：stat 是小寫 "ok"，data 列為 [日期(ISO), 名稱, 說明]。
type twHolidayResponse struct {
	Stat   string     `json:"stat"`
	Title  string     `json:"title"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

type calendarEntry struct {
	Date time.Time
	Name string
	Desc string
}

type calendarDay struct {
	IsOpen bool
	Note   string
}

// EnsureCalendarYear 確保某年份的交易日曆存在；已存在整年資料時不重抓。
// 規則：週末與公告假日 is_open=false，平日 true；
// 「開始交易日／最後交易日」屬於開市日註記，不是休市。
func (p *Pipeline) EnsureCalendarYear(ctx context.Context, year int) error {
	var count int
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := yearStart.AddDate(1, 0, 0)
	err := p.db.QueryRow(ctx, `
		SELECT count(*) FROM trading_calendar
		WHERE market = 'TWSE' AND cal_date >= $1::date AND cal_date < $2::date
	`, yearStart, yearEnd).Scan(&count)
	if err != nil {
		return err
	}
	daysInYear := int(yearEnd.Sub(yearStart).Hours() / 24)
	if count >= daysInYear {
		return nil
	}
	return p.ImportCalendarYear(ctx, year)
}

// ImportCalendarYear 從 TWSE 抓取某年份的開休市日期並重建該年交易日曆。
func (p *Pipeline) ImportCalendarYear(ctx context.Context, year int) error {
	url := fmt.Sprintf("%s?date=%d0101&response=json", p.cfg.TWSEHolidayQueryURL, year)
	dataTime := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	runID, err := p.beginIngestionRun(ctx, "twse", url, "TWSE 公開資料", "trading_calendar", &dataTime)
	if err != nil {
		return err
	}

	var payload twHolidayResponse
	checksum, err := p.fetchJSON(ctx, p.twseLimiter, url, &payload)
	if err != nil {
		_ = p.finishIngestionRun(ctx, runID, "failed", 0, "", err.Error(), nil)
		return err
	}
	if !strings.EqualFold(payload.Stat, "ok") {
		msg := fmt.Sprintf("TWSE 回應狀態異常（%d 年度可能尚未公告）: %s", year, payload.Stat)
		_ = p.finishIngestionRun(ctx, runID, "failed", 0, checksum, msg, nil)
		return fmt.Errorf("%s", msg)
	}

	entries := make([]calendarEntry, 0, len(payload.Data))
	for _, row := range payload.Data {
		if len(row) < 2 {
			continue
		}
		date, err := time.Parse("2006-01-02", strings.TrimSpace(row[0]))
		if err != nil {
			continue
		}
		desc := ""
		if len(row) >= 3 {
			desc = row[2]
		}
		entries = append(entries, calendarEntry{Date: date, Name: strings.TrimSpace(row[1]), Desc: desc})
	}

	days := buildYearCalendar(year, entries)
	tx, err := p.db.Begin(ctx)
	if err != nil {
		_ = p.finishIngestionRun(ctx, runID, "failed", 0, checksum, err.Error(), nil)
		return err
	}
	defer tx.Rollback(ctx)
	saved := 0
	for date := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC); date.Year() == year; date = date.AddDate(0, 0, 1) {
		day := days[date]
		if _, err := tx.Exec(ctx, `
			INSERT INTO trading_calendar (market, cal_date, is_open, note)
			VALUES ('TWSE', $1::date, $2, $3)
			ON CONFLICT (market, cal_date) DO UPDATE SET is_open = EXCLUDED.is_open, note = EXCLUDED.note
		`, date.Format("2006-01-02"), day.IsOpen, day.Note); err != nil {
			_ = p.finishIngestionRun(ctx, runID, "failed", saved, checksum, err.Error(), nil)
			return err
		}
		saved++
	}
	if err := tx.Commit(ctx); err != nil {
		_ = p.finishIngestionRun(ctx, runID, "failed", saved, checksum, err.Error(), nil)
		return err
	}
	p.log.Info("交易日曆匯入完成", "year", year, "days", saved, "holidays", len(entries))
	return p.finishIngestionRun(ctx, runID, "succeeded", saved, checksum, "", &dataTime)
}

// buildYearCalendar 由假日公告組出整年的開休市狀態（純函式，可單元測試）。
func buildYearCalendar(year int, entries []calendarEntry) map[time.Time]calendarDay {
	days := make(map[time.Time]calendarDay)
	for date := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC); date.Year() == year; date = date.AddDate(0, 0, 1) {
		weekday := date.Weekday()
		if weekday == time.Saturday || weekday == time.Sunday {
			days[date] = calendarDay{IsOpen: false, Note: "週末"}
		} else {
			days[date] = calendarDay{IsOpen: true}
		}
	}
	for _, entry := range entries {
		date := time.Date(entry.Date.Year(), entry.Date.Month(), entry.Date.Day(), 0, 0, 0, 0, time.UTC)
		if date.Year() != year {
			continue
		}
		existing := days[date]
		// 「國曆新年開始交易日」「農曆春節前最後交易日」是開市日的註記，不是休市日。
		if strings.Contains(entry.Name, "開始交易") || strings.Contains(entry.Name, "最後交易") {
			days[date] = calendarDay{IsOpen: existing.IsOpen, Note: entry.Name}
			continue
		}
		days[date] = calendarDay{IsOpen: false, Note: entry.Name}
	}
	return days
}

// LastOpenTradingDay 回傳 onOrBefore（含）之前最近的一個開市日。
func (s *Service) LastOpenTradingDay(ctx context.Context, market string, onOrBefore time.Time) (time.Time, error) {
	var day time.Time
	err := s.db.QueryRow(ctx, `
		SELECT cal_date FROM trading_calendar
		WHERE market = $1 AND is_open AND cal_date <= $2::date
		ORDER BY cal_date DESC
		LIMIT 1
	`, market, onOrBefore.Format("2006-01-02")).Scan(&day)
	if err != nil {
		return time.Time{}, err
	}
	return day, nil
}

// TradingDaysBetween 回傳 [from, to] 區間內的所有開市日（依日期遞增）。
func (s *Service) TradingDaysBetween(ctx context.Context, market string, from, to time.Time) ([]time.Time, error) {
	rows, err := s.db.Query(ctx, `
		SELECT cal_date FROM trading_calendar
		WHERE market = $1 AND is_open AND cal_date >= $2::date AND cal_date <= $3::date
		ORDER BY cal_date
	`, market, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []time.Time
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		days = append(days, day)
	}
	return days, rows.Err()
}

// IsTradingDay 查詢某日是否為開市日。found=false 表示日曆沒有該日資料（尚未匯入該年度）。
func (s *Service) IsTradingDay(ctx context.Context, market string, date time.Time) (isOpen, found bool, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT is_open FROM trading_calendar WHERE market = $1 AND cal_date = $2::date
	`, market, date.Format("2006-01-02")).Scan(&isOpen)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return isOpen, true, nil
}
