package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/tingz/easy-invest/internal/marketdata"
)

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.market.ListAssets(r.Context(), r.URL.Query().Get("query"), r.URL.Query().Get("type"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "資產清單讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	asset, err := s.market.GetAsset(r.Context(), chi.URLParam(r, "id"))
	if marketdata.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到資產")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "資產讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleMarketBars(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.market.ListBars(r.Context(), r.URL.Query().Get("symbol"), r.URL.Query().Get("from"), r.URL.Query().Get("to"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "行情資料讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCorporateActions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT c.id::text, a.symbol, c.action_type, c.ex_date::text, c.record_date::text,
		       c.pay_date::text, c.cash_per_share::text, c.stock_ratio::text, c.details
		FROM corporate_actions c
		JOIN assets a ON a.id = c.asset_id
		WHERE ($1 = '' OR a.symbol = $1)
		  AND ($2 = '' OR c.ex_date >= $2::date)
		  AND ($3 = '' OR c.ex_date <= $3::date)
		ORDER BY c.ex_date DESC
		LIMIT 200
	`, r.URL.Query().Get("symbol"), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "公司行動資料讀取失敗")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, symbol, actionType, exDate string
		var recordDate, payDate, cash, ratio *string
		var details map[string]any
		if err := rows.Scan(&id, &symbol, &actionType, &exDate, &recordDate, &payDate, &cash, &ratio, &details); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "公司行動資料讀取失敗")
			return
		}
		items = append(items, map[string]any{
			"id": id, "symbol": symbol, "action_type": actionType, "ex_date": exDate,
			"record_date": recordDate, "pay_date": payDate, "cash_per_share": cash,
			"stock_ratio": ratio, "details": details,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT market, cal_date::text, is_open, note
		FROM trading_calendar
		WHERE ($1 = '' OR cal_date >= $1::date)
		  AND ($2 = '' OR cal_date <= $2::date)
		ORDER BY market, cal_date
		LIMIT 500
	`, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "交易日曆讀取失敗")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var market, date, note string
		var isOpen bool
		if err := rows.Scan(&market, &date, &isOpen, &note); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "交易日曆讀取失敗")
			return
		}
		items = append(items, map[string]any{"market": market, "date": date, "is_open": isOpen, "note": note})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleFreshness(w http.ResponseWriter, r *http.Request) {
	items, err := s.market.Freshness(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "資料新鮮度讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
