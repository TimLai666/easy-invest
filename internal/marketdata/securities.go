package marketdata

import (
	"context"
	"fmt"
	"strings"
)

// SyncSecuritiesList 同步上市櫃證券清單：抓上市與上櫃當日行情端點，只更新 assets
// （代號、名稱、市場、資產類別），不寫行情。每週執行一次即可。
func (p *Pipeline) SyncSecuritiesList(ctx context.Context) (ImportResult, error) {
	runID, err := p.beginIngestionRun(ctx, "twse_tpex_openapi",
		p.cfg.TWSEDailyAllURL+" + "+p.cfg.TPExDailyAllURL, "TWSE/TPEx OpenAPI", "securities_list", nil)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{IngestionRunID: runID, Status: "running"}

	type listing struct {
		market string
		symbol string
		name   string
	}
	var listings []listing

	var twseRows []twseDailyAllRow
	twseChecksum, err := p.fetchJSON(ctx, p.twseLimiter, p.cfg.TWSEDailyAllURL, &twseRows)
	if err != nil {
		return p.failImport(ctx, result, "", err)
	}
	for _, row := range twseRows {
		symbol := strings.TrimSpace(row.Code)
		if symbol == "" {
			continue
		}
		listings = append(listings, listing{market: "TWSE", symbol: symbol, name: strings.TrimSpace(row.Name)})
	}

	var tpexRows []tpexDailyRow
	if _, err := p.fetchJSON(ctx, p.tpexLimiter, p.cfg.TPExDailyAllURL, &tpexRows); err != nil {
		// 上櫃清單抓不到時仍保留上市部分，但整批標記失敗讓排程之後重試。
		p.log.Warn("上櫃證券清單抓取失敗，本次僅同步上市部分", "error", err)
	} else {
		for _, row := range tpexRows {
			symbol := strings.TrimSpace(row.Code)
			if symbol == "" {
				continue
			}
			listings = append(listings, listing{market: "TPEX", symbol: symbol, name: strings.TrimSpace(row.Name)})
		}
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return p.failImport(ctx, result, twseChecksum, err)
	}
	defer tx.Rollback(ctx)
	synced := 0
	for _, item := range listings {
		if _, err := upsertAsset(ctx, tx, item.market, item.symbol, item.name); err != nil {
			result.Rows = synced
			return p.failImport(ctx, result, twseChecksum, fmt.Errorf("同步證券 %s/%s 失敗: %w", item.market, item.symbol, err))
		}
		synced++
	}
	if err := tx.Commit(ctx); err != nil {
		result.Rows = synced
		return p.failImport(ctx, result, twseChecksum, err)
	}
	result.Rows = synced
	if err := p.finishIngestionRun(ctx, runID, "succeeded", synced, twseChecksum, "", nil); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	return result, nil
}
