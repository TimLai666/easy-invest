package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppEnv             string
	AppSecret          string
	HTTPAddr           string
	DatabaseURL        string
	EnableRegistration bool
	Version            string

	// 市場資料管線（端點實測結果見 docs/data-sources.md）。
	TWSEDailyAllURL     string
	TWSEStockDayURL     string
	TWSECorpActionsURL  string
	TWSEHolidayQueryURL string
	TPExDailyAllURL     string
	// MarketImportOnStart 為 true 時，worker 啟動先跑一次 catch-up（把缺口補齊到最新）。
	MarketImportOnStart bool
	// MarketBackfillMonths 是歷史回補窗口（月），詳見 marketdata.PipelineConfig。
	MarketBackfillMonths int
}

func Load() Config {
	return Config{
		AppEnv:             env("APP_ENV", "development"),
		AppSecret:          env("APP_SECRET", "dev-secret-change-me-change-me"),
		HTTPAddr:           env("HTTP_ADDR", ":8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://easy_invest:easy_invest@localhost:5432/easy_invest?sslmode=disable"),
		EnableRegistration: envBool("ENABLE_REGISTRATION", true),
		Version:            env("APP_VERSION", "dev"),

		TWSEDailyAllURL:      env("TWSE_DAILY_ALL_URL", "https://openapi.twse.com.tw/v1/exchangeReport/STOCK_DAY_ALL"),
		TWSEStockDayURL:      env("TWSE_STOCK_DAY_URL", "https://www.twse.com.tw/exchangeReport/STOCK_DAY"),
		TWSECorpActionsURL:   env("TWSE_CORP_ACTIONS_URL", "https://openapi.twse.com.tw/v1/exchangeReport/TWT48U_ALL"),
		TWSEHolidayQueryURL:  env("TWSE_HOLIDAY_QUERY_URL", "https://www.twse.com.tw/rwd/zh/holidaySchedule/holidaySchedule"),
		TPExDailyAllURL:      env("TPEX_DAILY_ALL_URL", "https://www.tpex.org.tw/openapi/v1/tpex_mainboard_daily_close_quotes"),
		MarketImportOnStart:  envBool("MARKET_IMPORT_ON_START", true),
		MarketBackfillMonths: envInt("MARKET_BACKFILL_MONTHS", 24),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
