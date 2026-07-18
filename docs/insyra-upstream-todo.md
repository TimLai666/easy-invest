# Insyra 上游缺口與 TODO

## 使用原則

Easy Invest 的分析、指標、時間序列、回測與資料轉換功能，優先使用 Insyra。實作前要先確認 Insyra 是否已有合適能力。

若 Insyra 做不到或不好用：

1. 到 Insyra 開 upstream issue。
2. 在本專案對應程式碼留下 `TODO(insyra#<issue-number>): ...`。
3. 在本文件補上紀錄。
4. 只做必要的暫時替代方案，避免本專案長出一套難以移除的平行分析框架。

## 能力驗證（2026-06-14，依官方 pkg.go.dev 與 Docs）

對 `github.com/HazelnutParadise/insyra` 公開 API 逐項查證後的結論：

**Insyra 已具備**

- DataList 描述統計：`Mean`、`Stdev`/`StdevP`、`Var`/`VarP`、`Median`、`Max`、`Min`、`GMean`、`MAD`、`IQR`、`Percentile`、`Quartile`、`Mode`、`Sum`。
- `stats` 子套件：偏度（skewness）、峰度（kurtosis）、動差計算。
- `finance` 套件：TVM、NPV/IRR、折舊、債券定價。
- DataTable + CCL：欄位轉換、聚合（GroupBy/Aggregate）、Pivot/Unpivot。
- I/O：CSV/Excel/Parquet 讀寫、`csvxl` 多 CSV 併為 Excel。

**Insyra 沒有（本專案目前自寫）**

- 夏普比率與年化、最大回撤、年化報酬等投組績效指標。
- 常態分布 CDF 與反函數（normPPF / normCDF）。
- 組合對稱交叉驗證（CSCV）、過擬合機率（PBO）、通縮夏普比率（DSR）。
- walk-forward 切片與樣本外彙總。

## 決策（依需求分類）

| 類別 | 需求 | Insyra 是否支援 | 決策 |
|---|---|---|---|
| 領域邏輯 | 台股費稅（`twmarket`）、FIFO 批次/流水（`ledger`）、再平衡策略（`strategy`） | 非 Insyra 範疇 | 留在本專案領域套件，不上拋、不需 issue |
| 金融績效指標 | Sharpe、最大回撤、年化報酬 | ✅ v0.3.0 `quant` | **已採用** `quant.SharpeRatio`（[insyra#175](https://github.com/HazelnutParadise/insyra/issues/175) 已關閉）；回測 Result 的金額型指標仍用 decimal 於 `harness.go` |
| 機率/分布 | normPPF、normCDF | ✅ v0.3.0 `stats` | **已採用** `stats.NormCDF` / `stats.NormPPF`（[insyra#176](https://github.com/HazelnutParadise/insyra/issues/176) 已關閉） |
| 過擬合診斷 | PBO/CSCV、Deflated Sharpe | ✅ v0.3.0 `quant` | **已採用** `quant.PBO` / `quant.DeflatedSharpeRatio`（[insyra#177](https://github.com/HazelnutParadise/insyra/issues/177) 已關閉） |
| 回測驗證 | walk-forward 切片與樣本外彙總 | ✅ v0.3.0 `quant.WalkForward` | 月切訓練/測試**編排保留**於 `walkforward.go`（與 `Simulate`/`strategy.Rebalance` 深度整合），統計核心改用 `quant`（[insyra#178](https://github.com/HazelnutParadise/insyra/issues/178) 已關閉） |
| 資料 I/O | 券商對帳 CSV 匯入、市場資料轉換/報表匯出 | ✅（ReadCSV/ToCSV/Excel/DataTable） | **建議採用 Insyra**：把 `internal/reconcile` 的券商 CSV 解析改用 `insyra.ReadCSV`（BOM/編碼/Excel 較穩健）。目前 CSV 解析器已有測試，待測試補強完成後再行重構，避免同時改動衝突 |

判斷依據：`internal/backtest` 的過擬合診斷是一個小而聚焦、且 Insyra 本就缺乏對應能力的數值模組（約 6 個統計 helper + 金融/機率函式），不屬於 AGENTS.md 所警示的「大型平行分析框架」；以自寫 float64 核心換取數值精確與零外部相依是審慎的工程取捨。但描述統計與資料 I/O 確實是 Insyra 的強項，未來相關功能以 Insyra 為預設。

## TODO 表

| 狀態 | Insyra issue | 影響功能 | 現況 |
|---|---|---|---|
| ✅ 已採用 | [insyra#175](https://github.com/HazelnutParadise/insyra/issues/175)（夏普/回撤/年化，已關閉） | `internal/backtest/overfit.go` | 改用 `quant.SharpeRatio` |
| ✅ 已採用 | [insyra#176](https://github.com/HazelnutParadise/insyra/issues/176)（normPPF/normCDF，已關閉） | `internal/backtest/overfit.go` | 改用 `stats.NormCDF` / `stats.NormPPF` |
| ✅ 已採用 | [insyra#177](https://github.com/HazelnutParadise/insyra/issues/177)（PBO/CSCV、DSR，已關閉） | `internal/backtest/overfit.go` | 改用 `quant.PBO` / `quant.DeflatedSharpeRatio` |
| ✅ 已採用 | [insyra#178](https://github.com/HazelnutParadise/insyra/issues/178)（walk-forward，已關閉） | `internal/backtest/walkforward.go` | 統計核心改用 `quant`；月切編排保留 |
| 規劃中 | —（採用，不需 issue） | `internal/reconcile` 券商 CSV 匯入 | 待改用 `insyra.ReadCSV`（待對帳測試補強後重構） |

## upstream issue 與採用結果

2026-06-14 提交 4 則 issue，Insyra 於 **v0.3.0（2026-07-18）** 全部實作並關閉：

1. [insyra#175](https://github.com/HazelnutParadise/insyra/issues/175) → `quant.SharpeRatio` / `MaxDrawdown` / `AnnualizedReturn`
2. [insyra#176](https://github.com/HazelnutParadise/insyra/issues/176) → `stats.NormCDF` / `stats.NormPPF`（轉公開）
3. [insyra#177](https://github.com/HazelnutParadise/insyra/issues/177) → `quant.PBO` / `quant.DeflatedSharpeRatio` / `ProbabilisticSharpeRatio`
4. [insyra#178](https://github.com/HazelnutParadise/insyra/issues/178) → `quant.WalkForward`

## 目前狀態

已升級到 `github.com/HazelnutParadise/insyra v0.3.0` 並把 `internal/backtest` 的統計核心
（夏普、最大回撤、PBO、通縮夏普、常態 CDF/反函數）改用 `quant` 與 `stats`。既有的
`overfit_test.go` / `walkforward_test.go` 以寫死的期望值作為等價性驗證，改用後測試全數通過，
確認 Insyra 與原自寫實作結果一致。walk-forward 的月切訓練/測試編排因與 `Simulate` /
`strategy.Rebalance` 深度整合而保留；回測 Result 的金額型指標仍以 decimal 計算。
下一步（選配）：券商 CSV 匯入改用 `insyra.ReadCSV`，未來分析/報表以 Insyra 為預設。
