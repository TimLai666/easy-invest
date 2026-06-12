package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tingz/easy-invest/internal/config"
	"github.com/tingz/easy-invest/internal/marketdata"
	"github.com/tingz/easy-invest/internal/platform"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	if err := platform.RunMigrations(ctx, cfg.DatabaseURL); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	db, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	log.Info("worker started")
	market := marketdata.NewService(db)
	if cfg.MarketImportOnStart {
		if result, err := market.ImportTWSEDailyAll(ctx, cfg.TWSEDailyAllURL); err != nil {
			log.Warn("market import failed", "error", err, "run_id", result.IngestionRunID)
		} else {
			log.Info("market import completed", "run_id", result.IngestionRunID, "rows", result.Rows)
		}
	}
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-ticker.C:
			if result, err := market.ImportTWSEDailyAll(ctx, cfg.TWSEDailyAllURL); err != nil {
				log.Warn("market import failed", "error", err, "run_id", result.IngestionRunID)
			} else {
				log.Info("market import completed", "run_id", result.IngestionRunID, "rows", result.Rows)
			}
		case <-stop:
			log.Info("worker stopped")
			return
		}
	}
}
