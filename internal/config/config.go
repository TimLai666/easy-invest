package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppEnv              string
	AppSecret           string
	HTTPAddr            string
	DatabaseURL         string
	EnableRegistration  bool
	TWSEDailyAllURL     string
	MarketImportOnStart bool
	Version             string
}

func Load() Config {
	return Config{
		AppEnv:              env("APP_ENV", "development"),
		AppSecret:           env("APP_SECRET", "dev-secret-change-me-change-me"),
		HTTPAddr:            env("HTTP_ADDR", ":8080"),
		DatabaseURL:         env("DATABASE_URL", "postgres://easy_invest:easy_invest@localhost:5432/easy_invest?sslmode=disable"),
		EnableRegistration:  envBool("ENABLE_REGISTRATION", true),
		TWSEDailyAllURL:     env("TWSE_DAILY_ALL_URL", "https://openapi.twse.com.tw/v1/exchangeReport/STOCK_DAY_ALL"),
		MarketImportOnStart: envBool("MARKET_IMPORT_ON_START", true),
		Version:             env("APP_VERSION", "dev"),
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
