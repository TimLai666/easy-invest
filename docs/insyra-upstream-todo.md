# Insyra 上游缺口與 TODO

## 使用原則

Easy Invest 的分析、指標、時間序列、回測與資料轉換功能，優先使用 Insyra。實作前要先確認 Insyra 是否已有合適能力。

若 Insyra 做不到或不好用：

1. 到 Insyra 開 upstream issue。
2. 在本專案對應程式碼留下 `TODO(insyra#<issue-number>): ...`。
3. 在本文件補上紀錄。
4. 只做必要的暫時替代方案，避免本專案長出一套難以移除的平行分析框架。

## TODO 格式

| 狀態 | Insyra issue | 影響功能 | 目前替代方案 | 移除 TODO 條件 |
|---|---|---|---|---|
| 尚未建立 | 尚無 | 尚未開始實作，沒有確認缺口 | 無 | 實作時逐項檢查 |

## 能力初步評估（2026-06，依公開文件，動工時逐項以目標版本原始碼驗證）

Insyra 的核心定位是 dataframe 式工作流：DataList/DataTable、統計摘要、CCL 衍生欄位、CSV/Excel/Parquet I/O、平行轉換。對本專案的對應：

| 需求 | 初步判斷 | 對策 |
|---|---|---|
| K 線與時間序列整理、欄位轉換、聚合 | 可用（DataTable + CCL） | 直接用 |
| 缺值、停牌、除權息資料清理 | 部分可用（通用清理可，市場語意需自寫） | `internal/marketdata` 處理語意，Insyra 做轉換 |
| 報酬率、波動率、回撤 | 基礎統計可用，金融專用函式待確認 | 動工時驗證；缺則開 issue |
| Modified Dietz、XIRR、TWR | 公開文件未見 | 預期開 upstream issue（M3） |
| 技術指標（MA、RSI 等） | 公開文件未見 | 預期開 upstream issue（M4+，目標權重策略 v1 不需要） |
| 批次成本與交易流水聚合 | 屬本專案領域邏輯 | 在 `internal/ledger` 實作，不上拋 |
| 回測切片、walk-forward | 公開文件未見 | M6 開 issue；harness 在 `internal/backtest`，僅資料結構層借助 Insyra |
| GA 參數最佳化 | 公開文件未見 | M6 選配時再評估 |

注意：使用任何 Insyra API 或 CCL 語法前，必須先對 go.mod 鎖定版本驗證符號存在與簽名，不可憑印象寫。

## 目前狀態

尚未開始寫程式，上表是文件層級的初判，尚未建立任何 upstream issue。各里程碑（M3 績效、M4 指標、M6 回測）動工的第一步是驗證上表並把結果與 issue 連結回填到 TODO 表。
