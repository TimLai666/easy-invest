package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/tingz/easy-invest/internal/config"
	"github.com/tingz/easy-invest/internal/marketdata"
	"github.com/tingz/easy-invest/internal/platform"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	pipelineCfg := marketdata.DefaultPipelineConfig()
	pipelineCfg.TWSEDailyAllURL = cfg.TWSEDailyAllURL
	pipelineCfg.TWSEStockDayURL = cfg.TWSEStockDayURL
	pipelineCfg.TWSECorpActionsURL = cfg.TWSECorpActionsURL
	pipelineCfg.TWSEHolidayQueryURL = cfg.TWSEHolidayQueryURL
	pipelineCfg.TPExDailyAllURL = cfg.TPExDailyAllURL
	pipelineCfg.BackfillMonths = cfg.MarketBackfillMonths
	pipeline := marketdata.NewPipeline(db, pipelineCfg, log)
	svc := pipeline.Service()

	taipei, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		taipei = time.FixedZone("Asia/Taipei", 8*60*60)
	}

	// catch-up 不可重入：啟動補抓與排程補抓撞在一起時，後到的直接跳過。
	var catchUpMu sync.Mutex
	runCatchUp := func(trigger string) {
		if !catchUpMu.TryLock() {
			log.Info("catch-up 已在執行中，跳過本次觸發", "trigger", trigger)
			return
		}
		defer catchUpMu.Unlock()
		log.Info("catch-up 開始", "trigger", trigger)
		if err := pipeline.RunCatchUp(ctx); err != nil {
			log.Warn("catch-up 失敗", "trigger", trigger, "error", err)
			return
		}
		log.Info("catch-up 完成", "trigger", trigger)
	}

	scheduler := cron.New(
		cron.WithLocation(taipei),
		cron.WithChain(cron.SkipIfStillRunning(cronLogger{log: log})),
	)

	// 每個交易日 15:30（台北時間，日終資料定版後）：當日匯入 + catch-up。假日不誤跑。
	if _, err := scheduler.AddFunc("30 15 * * 1-5", func() {
		today := time.Now().In(taipei)
		isOpen, found, err := svc.IsTradingDay(ctx, "TWSE", today)
		if err != nil {
			log.Warn("交易日曆查詢失敗，仍嘗試執行（匯入本身冪等）", "error", err)
		} else if found && !isOpen {
			log.Info("今日休市，跳過日終匯入", "date", today.Format("2006-01-02"))
			return
		}
		runCatchUp("daily_1530")
	}); err != nil {
		log.Error("排程註冊失敗", "job", "daily_1530", "error", err)
		os.Exit(1)
	}

	// 每週日 08:00：同步證券清單；12 月時順便確保下一年度交易日曆已入庫。
	if _, err := scheduler.AddFunc("0 8 * * 0", func() {
		if result, err := pipeline.SyncSecuritiesList(ctx); err != nil {
			log.Warn("證券清單同步失敗", "error", err, "run_id", result.IngestionRunID)
		} else {
			log.Info("證券清單同步完成", "rows", result.Rows)
		}
		now := time.Now().In(taipei)
		if err := pipeline.EnsureCalendarYear(ctx, now.Year()); err != nil {
			log.Warn("年度交易日曆檢查失敗", "year", now.Year(), "error", err)
		}
		if now.Month() == time.December {
			if err := pipeline.EnsureCalendarYear(ctx, now.Year()+1); err != nil {
				log.Warn("下一年度交易日曆尚無法取得（可能尚未公告）", "year", now.Year()+1, "error", err)
			}
		}
	}); err != nil {
		log.Error("排程註冊失敗", "job", "weekly_sunday", "error", err)
		os.Exit(1)
	}

	scheduler.Start()
	log.Info("worker started", "import_on_start", cfg.MarketImportOnStart)

	// MARKET_IMPORT_ON_START：啟動先跑一次 catch-up，停機多久都能把缺口補回來。
	if cfg.MarketImportOnStart {
		go runCatchUp("startup")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("worker stopping")
	cancel() // 取消進行中的 catch-up（rate limiter 等待會立即返回）
	stopCtx := scheduler.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(30 * time.Second):
		log.Warn("排程工作未在期限內結束，強制離開")
	}
	log.Info("worker stopped")
}

// cronLogger 把 robfig/cron 的記錄轉接到 slog。
type cronLogger struct {
	log *slog.Logger
}

func (c cronLogger) Info(msg string, keysAndValues ...interface{}) {
	c.log.Debug(msg, keysAndValues...)
}

func (c cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	args := append([]any{"error", err}, keysAndValues...)
	c.log.Error(msg, args...)
}
