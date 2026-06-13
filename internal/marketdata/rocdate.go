package marketdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// taipeiLocation 回傳台北時區；載入失敗時退回固定 UTC+8（無夏令時間，語意相同）。
func taipeiLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("Asia/Taipei", 8*60*60)
	}
	return loc
}

// ParseROCCompactDate 解析民國年緊湊日期，例如 "1150611" → 2026-06-11。
// TWSE OpenAPI（STOCK_DAY_ALL、TWT48U_ALL、holidaySchedule）與 TPEx OpenAPI 都用這個格式。
func ParseROCCompactDate(value string) (time.Time, error) {
	cleaned := strings.TrimSpace(value)
	if len(cleaned) != 7 {
		return time.Time{}, fmt.Errorf("民國年日期長度不正確: %q", value)
	}
	year, err := strconv.Atoi(cleaned[:3])
	if err != nil {
		return time.Time{}, fmt.Errorf("民國年解析失敗: %q", value)
	}
	month, err := strconv.Atoi(cleaned[3:5])
	if err != nil {
		return time.Time{}, fmt.Errorf("民國年月份解析失敗: %q", value)
	}
	day, err := strconv.Atoi(cleaned[5:7])
	if err != nil {
		return time.Time{}, fmt.Errorf("民國年日期解析失敗: %q", value)
	}
	return rocToTime(year, month, day, value)
}

// ParseROCSlashDate 解析民國年斜線日期，例如 "115/06/01" → 2026-06-01。
// TWSE 個股日成交資訊（STOCK_DAY）的資料列用這個格式。
func ParseROCSlashDate(value string) (time.Time, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("民國年斜線日期格式不正確: %q", value)
	}
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	day, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("民國年斜線日期解析失敗: %q", value)
	}
	return rocToTime(year, month, day, value)
}

func rocToTime(rocYear, month, day int, raw string) (time.Time, error) {
	if rocYear < 1 || rocYear > 300 {
		return time.Time{}, fmt.Errorf("民國年份超出合理範圍: %q", raw)
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("民國年月日超出合理範圍: %q", raw)
	}
	t := time.Date(rocYear+1911, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	// time.Date 會自動進位（例如 2 月 30 日變 3 月初），這裡視為格式錯誤。
	if int(t.Month()) != month || t.Day() != day {
		return time.Time{}, fmt.Errorf("民國年日期不存在: %q", raw)
	}
	return t, nil
}
