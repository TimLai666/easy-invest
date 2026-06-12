# API 規格（v1 草案）

所有功能先有 API，UI 只是 API 的客戶端。本文件是 M1–M5 實作的合約；實作時以 OpenAPI（`api/openapi.yaml`）為機器可讀真源，本文件描述設計決策與慣例，兩者衝突時以 OpenAPI 為準並回頭修本文件。

## 通用慣例

- Base path：`/api/v1`。
- 格式：JSON，UTF-8。時間戳 RFC 3339（UTC），日期 `YYYY-MM-DD`（台北時區語意）。
- 金額與數量在 JSON 中一律用**字串**傳遞（如 `"1000.855"`），避免浮點誤差。
- 分頁：`?limit=`（預設 50，上限 200）+ `?cursor=`，回應帶 `next_cursor`。
- 冪等：所有會建立資源的 POST 支援 `Idempotency-Key` header，同 key 重送回傳原結果。

### 認證

兩種方式，擇一：

1. **Session cookie**：UI 用。`POST /auth/login` 後取得 HttpOnly cookie，搭配 CSRF token。
2. **API key**：`Authorization: Bearer ei_<random>`。伺服器存 SHA-256 雜湊比對，更新 `last_used_at`。

API key scopes：

| scope | 涵蓋 |
|---|---|
| `ledger:read` | 查詢事件、庫存、批次、績效 |
| `ledger:write` | 新增/void 事件 |
| `market:read` | 行情、公司行動、日曆 |
| `recommendations:read` | 查詢建議 |
| `recommendations:run` | 觸發建議計算 |
| `reconciliation:write` | 匯入券商快照、處理差異 |
| `settings:read` / `settings:write` | 設定 |

API key 管理端點本身只接受 session 認證（避免用 key 自我擴權）。

### 錯誤格式

```json
{
  "error": {
    "code": "validation_failed",
    "message": "quantity 必須為正整數股數",
    "details": [{"field": "quantity", "issue": "must_be_positive"}]
  }
}
```

固定 code 集合（至少）：`unauthorized`、`forbidden`、`not_found`、`validation_failed`、`conflict`、`idempotency_replay`、`rate_limited`、`internal`。

### 單位規則

所有帶數量的請求必須同時給 `quantity` 與 `unit`（`"share"` 或 `"lot"`）。伺服器一律轉成股儲存；回應同時回 `quantity_shares` 與 `quantity_lots`（lots 可為小數，僅供顯示）。缺 `unit` 一律 `validation_failed`，不猜。

## 端點總覽

### Auth 與 API key（session）

```
POST   /auth/register            # 第一階段可用環境變數關閉註冊（單人自用）
POST   /auth/login
POST   /auth/logout
GET    /me
GET    /api-keys                 # 只回 prefix 與中繼資料，不回明文
POST   /api-keys                 # 回應一次性顯示完整 key
POST   /api-keys/{id}/revoke
POST   /api-keys/{id}/rotate     # 舊 key 設定寬限期後失效
```

### 資產與市場資料

```
GET    /assets?query=2330&type=tw_stock
GET    /assets/{id}
GET    /market/bars?symbol=2330&from=2026-01-01&to=2026-06-10
GET    /market/corporate-actions?symbol=2330&from=&to=
GET    /market/calendar?from=&to=
GET    /market/freshness          # 各 dataset 最後成功匯入時間，UI 顯示資料新鮮度
```

### Ledger

```
POST   /ledger/events
GET    /ledger/events?asset=2330&type=buy&from=&to=
GET    /ledger/events/{id}
POST   /ledger/events/{id}/void   # body: {"reason": "..."}；不可 DELETE、不可 PUT
```

新增買進範例：

```json
POST /api/v1/ledger/events
{
  "event_type": "buy",
  "symbol": "2330",
  "trade_date": "2026-06-10",
  "quantity": "1",
  "unit": "lot",
  "price": "1000",
  "fee": null,
  "notes": "看好先進製程"
}
```

回應（201）：

```json
{
  "id": "…",
  "event_type": "buy",
  "asset": {"symbol": "2330", "name": "台積電"},
  "trade_date": "2026-06-10",
  "settlement_date": "2026-06-12",
  "quantity_shares": "1000",
  "quantity_lots": "1",
  "price": "1000",
  "gross_amount": "1000000",
  "fee": "855",
  "fee_source": "estimated",
  "tax": "0",
  "cash_delta": "-1000855",
  "lot": {"id": "…", "original_quantity": "1000"}
}
```

### 庫存與績效

```
GET /portfolio                    # 目前持股、平均成本、市值、未實現損益、現金
GET /portfolio/lots?symbol=2330   # 批次明細（原始成本與調整後成本兩種視角）
GET /portfolio/history?from=&to=  # 每日市值序列（M3 之後）
GET /portfolio/performance?period=ytd   # 已實現/未實現、TWR 或 XIRR（M3+）
```

`GET /portfolio` 回應必附 `market_data_as_of`，標示市值用的是哪一天收盤價。

### 建議

```
POST /recommendations/runs        # 觸發一次計算；body 可覆寫策略參數
GET  /recommendations/runs?limit=10
GET  /recommendations/runs/{id}   # 含 items、inputs 摘要、資料時間
PATCH /recommendations/items/{id} # {"user_status": "accepted" | "ignored"}
```

run 回應範例（節錄）：

```json
{
  "id": "…",
  "strategy": {"name": "target_weight_rebalance", "version": "1.0.0"},
  "market_data_as_of": "2026-06-11",
  "portfolio_as_of": "2026-06-12T01:20:00Z",
  "disclaimer": "本內容為參考資訊，不構成投資建議…",
  "items": [
    {
      "action": "buy",
      "asset": {"symbol": "0050"},
      "quantity_shares": "2000",
      "est_price": "182.5",
      "est_amount": "365000",
      "est_fee_tax": "312",
      "current_weight": "0.31",
      "target_weight": "0.40",
      "reason": "0050 目前權重 31%，低於目標 40% 且超出 ±5% 再平衡區間…",
      "risks": "使用 2026-06-11 收盤價估算，市價可能偏離…"
    }
  ]
}
```

### 對帳

```
POST /reconciliation/broker-snapshots          # multipart 上傳券商 CSV 或 JSON body
GET  /reconciliation/broker-snapshots
POST /reconciliation/runs                      # body: {"broker_snapshot_id": "…"}
GET  /reconciliation/runs/{id}                 # 含 diffs
POST /reconciliation/diffs/{id}/resolve
     # body: {"resolution": "adjusted", "adjustment": {…ledger event 欄位…}}
     # resolution = adjusted 時，伺服器在同交易內建立 adjustment 事件並回填關聯
```

### 設定與系統

```
GET  /settings
PUT  /settings
GET  /strategies                  # 可用策略與版本
GET  /healthz                     # 不需認證；DB 連線與 migration 版本檢查
GET  /version
```

## 安全要求

- 全站 HTTPS（部署層處理）；cookie 設 `Secure` + `HttpOnly` + `SameSite=Lax`。
- 密碼 argon2id；登入與 API key 驗證做 rate limit（如每 IP 每分鐘 10 次失敗）。
- API key 明文只在建立當下回傳一次。
- 所有寫入動作記 `audit_log`。
- 回應不得洩漏他人資料：所有查詢強制掛 `user_id` 條件（repository 層統一處理）。
