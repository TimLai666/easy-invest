package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/ledger"
)

type createLedgerEventRequest struct {
	EventType      string         `json:"event_type"`
	Symbol         string         `json:"symbol"`
	TradeDate      string         `json:"trade_date"`
	SettlementDate *string        `json:"settlement_date"`
	Quantity       string         `json:"quantity"`
	Unit           string         `json:"unit"`
	Price          string         `json:"price"`
	GrossAmount    *string        `json:"gross_amount"`
	Fee            *string        `json:"fee"`
	Tax            *string        `json:"tax"`
	Source         string         `json:"source"`
	SourceRef      *string        `json:"source_ref"`
	Notes          string         `json:"notes"`
	Metadata       map[string]any `json:"metadata"`
}

func (s *Server) handleCreateLedgerEvent(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req createLedgerEventRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	event, err := s.ledger.CreateEvent(r.Context(), ledger.CreateEventInput{
		UserID: principal.UserID, EventType: req.EventType, Symbol: req.Symbol,
		TradeDate: req.TradeDate, SettlementDate: req.SettlementDate,
		Quantity: req.Quantity, Unit: req.Unit, Price: req.Price,
		GrossAmount: req.GrossAmount, Fee: req.Fee, Tax: req.Tax,
		Source: req.Source, SourceRef: req.SourceRef, Notes: req.Notes, Metadata: req.Metadata,
	})
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "ledger.create", "ledger_events", event.ID, map[string]any{"event_type": event.EventType})
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleListLedgerEvents(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.ledger.ListEvents(r.Context(), principal.UserID, r.URL.Query().Get("asset"), r.URL.Query().Get("type"), r.URL.Query().Get("from"), r.URL.Query().Get("to"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "交易流水讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleGetLedgerEvent(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	event, err := s.ledger.GetEvent(r.Context(), principal.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "找不到交易事件")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "交易事件讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

type voidRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleVoidLedgerEvent(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var req voidRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	err := s.ledger.VoidEvent(r.Context(), principal.UserID, chi.URLParam(r, "id"), req.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "找不到可 void 的交易事件")
		return
	}
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	_ = s.auth.Audit(r.Context(), principal, "ledger.void", "ledger_events", chi.URLParam(r, "id"), map[string]any{"reason": req.Reason})
	writeNoContent(w)
}

func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	portfolio, err := s.ledger.Portfolio(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "庫存讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, portfolio)
}

func (s *Server) handlePortfolioHistory(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.ledger.PortfolioHistory(r.Context(), principal.UserID, r.URL.Query().Get("from"), r.URL.Query().Get("to"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "庫存歷史讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleLots(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	lots, err := s.ledger.Lots(r.Context(), principal.UserID, r.URL.Query().Get("symbol"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "批次讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": lots})
}

func writeLedgerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ledger.ErrValidation):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, ledger.ErrInsufficientShares):
		writeError(w, http.StatusConflict, "conflict", "可用股數不足，無法建立賣出事件")
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", "找不到資產或資料")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "交易流水處理失敗")
	}
}
