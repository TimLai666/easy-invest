package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/recommend"
)

func (s *Server) handleCreateRecommendationRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var raw map[string]any
	if r.Body != http.NoBody {
		_ = json.NewDecoder(r.Body).Decode(&raw)
	}
	override := recommend.Override{}
	if v, ok := raw["target_weights"]; ok {
		b, _ := json.Marshal(v)
		var weights map[string]any
		_ = json.Unmarshal(b, &weights)
		override.TargetWeights = map[string]decimal.Decimal{}
		for symbol, value := range weights {
			override.TargetWeights[symbol] = num.Parse(str(value))
		}
	}
	run, err := s.recommend.CreateRun(r.Context(), principal.UserID, override)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "建議產生失敗")
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "recommendation.run", "recommendation_runs", run.ID, nil)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleListRecommendationRuns(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.recommend.ListRuns(r.Context(), principal.UserID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "建議紀錄讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *Server) handleGetRecommendationRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	run, err := s.recommend.GetRun(r.Context(), principal.UserID, chi.URLParam(r, "id"))
	if recommend.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到建議紀錄")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "建議紀錄讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type patchRecommendationItemRequest struct {
	UserStatus string `json:"user_status"`
}

func (s *Server) handlePatchRecommendationItem(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req patchRecommendationItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	item, err := s.recommend.UpdateItemStatus(r.Context(), principal.UserID, chi.URLParam(r, "id"), req.UserStatus)
	if recommend.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到建議項目")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "user_status 不合法")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
