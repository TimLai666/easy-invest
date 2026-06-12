package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/reconcile"
)

func (s *Server) handleCreateBrokerSnapshot(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req reconcile.CreateSnapshotInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	req.UserID = principal.UserID
	snapshot, err := s.reconcile.CreateBrokerSnapshot(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "reconciliation.snapshot.create", "broker_snapshots", snapshot.ID, nil)
	writeJSON(w, http.StatusCreated, snapshot)
}

func (s *Server) handleListBrokerSnapshots(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := s.reconcile.ListBrokerSnapshots(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "券商快照讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createReconciliationRunRequest struct {
	BrokerSnapshotID string `json:"broker_snapshot_id"`
}

func (s *Server) handleCreateReconciliationRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req createReconciliationRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	run, err := s.reconcile.CreateRun(r.Context(), principal.UserID, req.BrokerSnapshotID)
	if reconcile.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到券商快照")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "對帳建立失敗")
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "reconciliation.run.create", "reconciliation_runs", run.ID, nil)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleGetReconciliationRun(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	run, err := s.reconcile.GetRun(r.Context(), principal.UserID, chi.URLParam(r, "id"))
	if reconcile.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到對帳紀錄")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "對帳紀錄讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type resolveDiffRequest struct {
	Resolution string `json:"resolution"`
}

func (s *Server) handleResolveReconciliationDiff(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req resolveDiffRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	diff, err := s.reconcile.ResolveDiff(r.Context(), principal.UserID, chi.URLParam(r, "id"), req.Resolution)
	if reconcile.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "找不到對帳差異")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "resolution 不合法")
		return
	}
	writeJSON(w, http.StatusOK, diff)
}
