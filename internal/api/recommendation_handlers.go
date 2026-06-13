package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/recommend"
)

type createRecommendationRunRequest struct {
	TargetWeights map[string]json.RawMessage `json:"target_weights,omitempty"`
}

func (s *Server) handleCreateRecommendationRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req createRecommendationRunRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	override, detail, err := req.override()
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), detail)
		return
	}
	run, err := s.recommend.CreateRun(r.Context(), principal.UserID, override)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "建議產生失敗")
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "recommendation.run", "recommendation_runs", run.ID, nil)
	writeJSON(w, http.StatusCreated, run)
}

func (req createRecommendationRunRequest) override() (recommend.Override, ErrorDetail, error) {
	override := recommend.Override{}
	if len(req.TargetWeights) == 0 {
		return override, ErrorDetail{}, nil
	}
	override.TargetWeights = make(map[string]decimal.Decimal, len(req.TargetWeights))
	for symbol, raw := range req.TargetWeights {
		if symbol == "" {
			return recommend.Override{}, ErrorDetail{Field: "target_weights", Issue: "symbol_required"}, errValidation("target_weights 的標的代碼不可為空")
		}
		weight, err := parseWeight(raw)
		if err != nil {
			return recommend.Override{}, ErrorDetail{Field: "target_weights." + symbol, Issue: "must_be_decimal_string_or_number"}, err
		}
		if weight.IsNegative() || weight.GreaterThan(decimal.NewFromInt(1)) {
			return recommend.Override{}, ErrorDetail{Field: "target_weights." + symbol, Issue: "must_be_between_0_and_1"}, errValidation("target_weights 必須介於 0 到 1")
		}
		override.TargetWeights[symbol] = weight
	}
	return override, ErrorDetail{}, nil
}

func parseWeight(raw json.RawMessage) (decimal.Decimal, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		weight, err := decimal.NewFromString(text)
		if err != nil {
			return decimal.Decimal{}, errValidation("target_weights 必須是 decimal 字串或數字")
		}
		return weight, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		weight, err := decimal.NewFromString(number.String())
		if err != nil {
			return decimal.Decimal{}, errValidation("target_weights 必須是 decimal 字串或數字")
		}
		return weight, nil
	}
	return decimal.Decimal{}, errValidation("target_weights 必須是 decimal 字串或數字")
}

type validationError string

func (e validationError) Error() string { return string(e) }

func errValidation(message string) error { return validationError(message) }

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
