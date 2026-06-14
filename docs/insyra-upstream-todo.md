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
| 描述統計 | mean/std/var/skew/kurt | ✅（main + `stats`） | 回測診斷的 `[]float64` 數值核心保留小型內聯 helper，避免把核心耦合到 Insyra 的 `any` 盒裝 DataList 與其相依樹；**未來使用者面向的分析/報表（績效歸因、因子分析、資料探索）一律改用 Insyra 為預設** |
| 金融績效指標 | Sharpe、最大回撤、年化報酬 | ❌ | 自寫於 `internal/backtest`，已開 [insyra#175](https://github.com/HazelnutParadise/insyra/issues/175)，留 `TODO(insyra#175)` |
| 機率/分布 | normPPF、normCDF | ❌ | 自寫（Acklam 近似 + `math.Erfc`）於 `internal/backtest/overfit.go`，已開 [insyra#176](https://github.com/HazelnutParadise/insyra/issues/176) |
| 過擬合診斷 | PBO/CSCV、Deflated Sharpe | ❌ | 自寫於 `internal/backtest/overfit.go`，已開 [insyra#177](https://github.com/HazelnutParadise/insyra/issues/177) |
| 回測驗證 | walk-forward 切片與樣本外彙總 | ❌ | 自寫於 `internal/backtest/walkforward.go`，已開 [insyra#178](https://github.com/HazelnutParadise/insyra/issues/178) |
| 資料 I/O | 券商對帳 CSV 匯入、市場資料轉換/報表匯出 | ✅（ReadCSV/ToCSV/Excel/DataTable） | **建議採用 Insyra**：把 `internal/reconcile` 的券商 CSV 解析改用 `insyra.ReadCSV`（BOM/編碼/Excel 較穩健）。目前 CSV 解析器已有測試，待測試補強完成後再行重構，避免同時改動衝突 |

判斷依據：`internal/backtest` 的過擬合診斷是一個小而聚焦、且 Insyra 本就缺乏對應能力的數值模組（約 6 個統計 helper + 金融/機率函式），不屬於 AGENTS.md 所警示的「大型平行分析框架」；以自寫 float64 核心換取數值精確與零外部相依是審慎的工程取捨。但描述統計與資料 I/O 確實是 Insyra 的強項，未來相關功能以 Insyra 為預設。

## TODO 表

| 狀態 | Insyra issue | 影響功能 | 目前替代方案 | 移除 TODO 條件 |
|---|---|---|---|---|
| 已提交 | [insyra#175](https://github.com/HazelnutParadise/insyra/issues/175)（夏普/回撤/年化） | `internal/backtest`（harness、walkforward） | 自寫 float64 指標 | Insyra `finance`/`stats` 提供投組績效指標且簽名穩定後改用 |
| 已提交 | [insyra#176](https://github.com/HazelnutParadise/insyra/issues/176)（normPPF/normCDF） | `internal/backtest/overfit.go` | Acklam 反函數 + `math.Erfc` | Insyra 提供常態 CDF/反函數後改用 |
| 已提交 | [insyra#177](https://github.com/HazelnutParadise/insyra/issues/177)（PBO/CSCV、DSR） | `internal/backtest/overfit.go` | 自寫 CSCV 與通縮夏普 | Insyra 提供等價回測過擬合診斷後改用 |
| 已提交 | [insyra#178](https://github.com/HazelnutParadise/insyra/issues/178)（walk-forward） | `internal/backtest/walkforward.go` | 自寫滑動視窗與樣本外彙總 | Insyra 提供 walk-forward 工具後改用 |
| 規劃中 | —（採用，不需 issue） | `internal/reconcile` 券商 CSV 匯入 | 自寫 CSV 解析器 | 改用 `insyra.ReadCSV`（待對帳測試補強後重構） |

## 已提交的 upstream issue（2026-06-14）

1. [insyra#175](https://github.com/HazelnutParadise/insyra/issues/175) — 投組績效指標：Sharpe、最大回撤、年化報酬。
2. [insyra#176](https://github.com/HazelnutParadise/insyra/issues/176) — 常態分布 CDF 與反函數（PPF / quantile）。
3. [insyra#177](https://github.com/HazelnutParadise/insyra/issues/177) — 回測過擬合診斷：CSCV PBO 與 Deflated Sharpe Ratio。
4. [insyra#178](https://github.com/HazelnutParadise/insyra/issues/178) — 時間序列 walk-forward 樣本外驗證工具。

## 目前狀態

M2–M6 已實作。回測與過擬合診斷（`internal/backtest`）因 Insyra 缺乏對應金融/機率/驗證能力而自寫，已開上述 4 則 upstream issue，並在程式碼留下對應的 `TODO(insyra#175/176/177/178)`。描述統計與 CSV/Excel I/O 為 Insyra 強項，券商 CSV 匯入規劃改用 Insyra（待對帳測試補強後重構）；未來分析/報表功能一律以 Insyra 為預設。待上游實作後，依各 issue 移除 TODO 並改用 Insyra。
