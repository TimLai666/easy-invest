package api

import (
	"encoding/json"
	"net/http"

	"github.com/tingz/easy-invest/internal/auth"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	settings, err := s.readSettings(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "設定讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req map[string]any
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	targetWeights, _ := json.Marshal(req["target_weights"])
	_, err := s.db.Exec(r.Context(), `
		UPDATE user_settings
		SET fee_rate = COALESCE(NULLIF($2, '')::numeric, fee_rate),
		    fee_discount = COALESCE(NULLIF($3, '')::numeric, fee_discount),
		    fee_minimum = COALESCE(NULLIF($4, '')::numeric, fee_minimum),
		    dividend_transfer_fee = COALESCE(NULLIF($5, '')::numeric, dividend_transfer_fee),
		    cash_buffer = COALESCE(NULLIF($6, '')::numeric, cash_buffer),
		    min_trade_amount = COALESCE(NULLIF($7, '')::numeric, min_trade_amount),
		    prefer_whole_lot = COALESCE($8, prefer_whole_lot),
		    risk_profile = COALESCE(NULLIF($9, ''), risk_profile),
		    target_weights = CASE WHEN $10::jsonb = 'null'::jsonb THEN target_weights ELSE $10::jsonb END,
		    rebalance_band = COALESCE(NULLIF($11, '')::numeric, rebalance_band),
		    updated_at = now()
		WHERE user_id = $1
	`, principal.UserID, str(req["fee_rate"]), str(req["fee_discount"]), str(req["fee_minimum"]),
		str(req["dividend_transfer_fee"]), str(req["cash_buffer"]), str(req["min_trade_amount"]),
		boolPtr(req["prefer_whole_lot"]), str(req["risk_profile"]), string(targetWeights), str(req["rebalance_band"]))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "設定格式錯誤")
		return
	}
	settings, err := s.readSettings(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "設定讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleStrategies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id::text, name, version, description, created_at FROM strategy_versions ORDER BY name, version`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "策略讀取失敗")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, name, version, description string
		var createdAt any
		if err := rows.Scan(&id, &name, &version, &description, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "策略讀取失敗")
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "version": version, "description": description, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func boolPtr(v any) *bool {
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}
