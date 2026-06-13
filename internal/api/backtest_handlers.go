package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/backtest"
)

func (s *Server) handleCreateBacktestRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req backtest.RunParams
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	run, err := s.backtest.CreateRun(r.Context(), principal.UserID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "backtest.run", "backtest_runs", run.ID, nil)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleListBacktestRuns(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.backtest.ListRuns(r.Context(), principal.UserID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "回測紀錄讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *Server) handleGetBacktestRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	run, err := s.backtest.GetRun(r.Context(), principal.UserID, chi.URLParam(r, "id"))
	if backtest.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到回測紀錄")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "回測紀錄讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, run)
}
