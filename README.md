# Easy Invest

Easy Invest 是以台股為主、API-first 的投資建議系統。系統以交易流水推導庫存，結合市場資料與策略邏輯產生可追蹤的投資建議；預設不直接替使用者買賣。

## 文件

- `AGENTS.md`：AI agent 與後續開發規範。
- `docs/product-requirements.md`：產品需求與範圍。
- `docs/architecture.md`：系統架構。
- `docs/architecture-diagram.md`：系統架構圖。
- `docs/data-and-ledger.md`：交易流水、庫存與對帳模型。
- `docs/data-sources.md`：台股資料來源與資料新鮮度規則。
- `docs/insyra-upstream-todo.md`：Insyra 上游缺口與 TODO 紀錄。
- `docs/reference/`：外部參考文件的繁中整理版。

## 後端啟動

本專案後端已具備 Go API server、worker、PostgreSQL schema migration 與 Docker Compose。

```bash
cp .env.example .env
docker compose up -d
curl http://localhost:8080/healthz
```

API base path 是 `/api/v1`。第一次使用可先註冊、登入，再建立 API key：

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"me@example.com","password":"passw0rd-long","display_name":"Me"}'
```

目前後端已包含：

- session 登入與 API key 管理，API key 只儲存雜湊值。
- 資產查詢、台股費稅估算、交易流水、FIFO 批次、void 後重建批次。
- `GET /portfolio` 庫存查詢與 portfolio snapshot。
- 目標權重再平衡建議引擎，保存 recommendation run/items 並附資料時間與免責聲明。
- 券商快照匯入與基本股數差異對帳。
- worker 會定期嘗試匯入 TWSE OpenAPI 當日全市場日終行情，成功或失敗都會留下 `ingestion_runs` 紀錄。

本機驗證：

```bash
go test ./...
go vet ./...
docker compose config
```
