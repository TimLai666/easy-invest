package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tingz/easy-invest/internal/config"
)

type endpointCheck struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Rows int    `json:"rows"`
	Note string `json:"note,omitempty"`
}

type twseDailyAllRow struct {
	Date         string `json:"Date"`
	Code         string `json:"Code"`
	Name         string `json:"Name"`
	ClosingPrice string `json:"ClosingPrice"`
}

type twseStockDayResponse struct {
	Stat   string     `json:"stat"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

type twseCorpActionRow struct {
	Date       string `json:"Date"`
	Code       string `json:"Code"`
	Name       string `json:"Name"`
	Exdividend string `json:"Exdividend"`
}

type twseHolidayResponse struct {
	Stat   string     `json:"stat"`
	Title  string     `json:"title"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

type tpexDailyRow struct {
	Date  string `json:"Date"`
	Code  string `json:"SecuritiesCompanyCode"`
	Name  string `json:"CompanyName"`
	Close string `json:"Close"`
}

type tpexStockDayResponse struct {
	Stat   string              `json:"stat"`
	Tables []tpexStockDayTable `json:"tables"`
}

type tpexStockDayTable struct {
	Title  string     `json:"title"`
	Date   string     `json:"date"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

func main() {
	twseMonth := flag.String("twse-month", "202401", "TWSE 歷史月資料查詢月份，格式 YYYYMM")
	twseSymbol := flag.String("twse-symbol", "0050", "TWSE 歷史月資料查詢代號")
	tpexMonth := flag.String("tpex-month", "2024/01/01", "TPEx 歷史月資料查詢日期，格式 YYYY/MM/01")
	tpexSymbol := flag.String("tpex-symbol", "006201", "TPEx 歷史月資料查詢代號")
	holidayYear := flag.Int("holiday-year", time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Year(), "TWSE 交易日曆查詢西元年")
	requestInterval := flag.Duration("request-interval", 4*time.Second, "兩次官方 API 呼叫之間的最小間隔")
	flag.Parse()

	cfg := config.Load()
	client := &http.Client{Timeout: 30 * time.Second}
	ctx := context.Background()

	var checks []endpointCheck
	checked, err := checkTWSEDailyAll(ctx, client, cfg.TWSEDailyAllURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	waitBetweenRequests(*requestInterval)
	twseStockURL := fmt.Sprintf("%s?response=json&date=%s01&stockNo=%s", cfg.TWSEStockDayURL, *twseMonth, *twseSymbol)
	checked, err = checkTWSEStockDay(ctx, client, twseStockURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	waitBetweenRequests(*requestInterval)
	checked, err = checkTWSECorpActions(ctx, client, cfg.TWSECorpActionsURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	waitBetweenRequests(*requestInterval)
	holidayURL := fmt.Sprintf("%s?date=%d0101&response=json", cfg.TWSEHolidayQueryURL, *holidayYear)
	checked, err = checkTWSEHoliday(ctx, client, holidayURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	waitBetweenRequests(*requestInterval)
	checked, err = checkTPExDailyAll(ctx, client, cfg.TPExDailyAllURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	waitBetweenRequests(*requestInterval)
	tpexStockURL := fmt.Sprintf("%s?date=%s&code=%s&response=json", cfg.TPExStockDayURL, *tpexMonth, *tpexSymbol)
	checked, err = checkTPExStockDay(ctx, client, tpexStockURL)
	if err != nil {
		fail(err)
	}
	checks = append(checks, checked)

	out := struct {
		VerifiedAt string          `json:"verified_at"`
		Checks     []endpointCheck `json:"checks"`
	}{
		VerifiedAt: time.Now().Format(time.RFC3339),
		Checks:     checks,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
}

func waitBetweenRequests(interval time.Duration) {
	if interval <= 0 {
		return
	}
	time.Sleep(interval)
}

func checkTWSEDailyAll(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var rows []twseDailyAllRow
	if err := fetchJSON(ctx, client, url, &rows); err != nil {
		return endpointCheck{}, err
	}
	if len(rows) == 0 {
		return endpointCheck{}, errors.New("TWSE STOCK_DAY_ALL 沒有回傳資料列")
	}
	for _, row := range rows {
		if row.Date == "" || row.Code == "" || row.Name == "" {
			return endpointCheck{}, errors.New("TWSE STOCK_DAY_ALL 欄位缺漏")
		}
		if row.Code == "0050" && row.ClosingPrice != "" {
			return endpointCheck{Name: "twse_daily_all", URL: url, Rows: len(rows), Note: "0050 close=" + row.ClosingPrice + " date=" + row.Date}, nil
		}
	}
	return endpointCheck{}, errors.New("TWSE STOCK_DAY_ALL 找不到 0050 或收盤價")
}

func checkTWSEStockDay(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var payload twseStockDayResponse
	if err := fetchJSON(ctx, client, url, &payload); err != nil {
		return endpointCheck{}, err
	}
	if !strings.EqualFold(payload.Stat, "OK") {
		return endpointCheck{}, fmt.Errorf("TWSE STOCK_DAY stat=%s", payload.Stat)
	}
	if len(payload.Data) == 0 || len(payload.Data[0]) < 9 {
		return endpointCheck{}, errors.New("TWSE STOCK_DAY 沒有足夠資料列")
	}
	return endpointCheck{Name: "twse_stock_day", URL: url, Rows: len(payload.Data), Note: "fields=" + strings.Join(payload.Fields, "|")}, nil
}

func checkTWSECorpActions(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var rows []twseCorpActionRow
	if err := fetchJSON(ctx, client, url, &rows); err != nil {
		return endpointCheck{}, err
	}
	if len(rows) == 0 {
		return endpointCheck{Name: "twse_corporate_actions", URL: url, Rows: 0, Note: "目前無除權息預告列"}, nil
	}
	if rows[0].Date == "" || rows[0].Code == "" || rows[0].Name == "" {
		return endpointCheck{}, errors.New("TWSE TWT48U_ALL 欄位缺漏")
	}
	return endpointCheck{Name: "twse_corporate_actions", URL: url, Rows: len(rows), Note: "sample=" + rows[0].Code + " " + rows[0].Name}, nil
}

func checkTWSEHoliday(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var payload twseHolidayResponse
	if err := fetchJSON(ctx, client, url, &payload); err != nil {
		return endpointCheck{}, err
	}
	if !strings.EqualFold(payload.Stat, "ok") {
		return endpointCheck{}, fmt.Errorf("TWSE holiday stat=%s", payload.Stat)
	}
	if len(payload.Data) == 0 || len(payload.Data[0]) < 3 {
		return endpointCheck{}, errors.New("TWSE holiday 沒有足夠資料列")
	}
	return endpointCheck{Name: "twse_holiday", URL: url, Rows: len(payload.Data), Note: payload.Title}, nil
}

func checkTPExDailyAll(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var rows []tpexDailyRow
	if err := fetchJSON(ctx, client, url, &rows); err != nil {
		return endpointCheck{}, err
	}
	if len(rows) == 0 {
		return endpointCheck{}, errors.New("TPEx daily close 沒有回傳資料列")
	}
	for _, row := range rows {
		if row.Date == "" || row.Code == "" || row.Name == "" {
			return endpointCheck{}, errors.New("TPEx daily close 欄位缺漏")
		}
		if row.Code == "006201" && row.Close != "" {
			return endpointCheck{Name: "tpex_daily_all", URL: url, Rows: len(rows), Note: "006201 close=" + row.Close + " date=" + row.Date}, nil
		}
	}
	return endpointCheck{}, errors.New("TPEx daily close 找不到 006201 或收盤價")
}

func checkTPExStockDay(ctx context.Context, client *http.Client, url string) (endpointCheck, error) {
	var payload tpexStockDayResponse
	if err := fetchJSON(ctx, client, url, &payload); err != nil {
		return endpointCheck{}, err
	}
	if !strings.EqualFold(payload.Stat, "ok") {
		return endpointCheck{}, fmt.Errorf("TPEx tradingStock stat=%s", payload.Stat)
	}
	for _, table := range payload.Tables {
		if len(table.Data) > 0 && len(table.Data[0]) >= 9 {
			return endpointCheck{Name: "tpex_stock_day", URL: url, Rows: len(table.Data), Note: "fields=" + strings.Join(table.Fields, "|")}, nil
		}
	}
	return endpointCheck{}, errors.New("TPEx tradingStock 沒有足夠資料列")
}

func fetchJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "easy-invest-verifier/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body[:min(len(body), 64)]))
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return fmt.Errorf("%s: 回應是 HTML，不是 JSON", url)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: JSON 解析失敗: %w", url, err)
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
