package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tingz/easy-invest/internal/auth"
	"github.com/tingz/easy-invest/internal/config"
	"github.com/tingz/easy-invest/internal/ledger"
	"github.com/tingz/easy-invest/internal/marketdata"
	"github.com/tingz/easy-invest/internal/platform"
	"github.com/tingz/easy-invest/internal/recommend"
	"github.com/tingz/easy-invest/internal/reconcile"
)

type Server struct {
	cfg       config.Config
	db        *pgxpool.Pool
	log       *slog.Logger
	auth      *auth.Service
	ledger    *ledger.Service
	market    *marketdata.Service
	recommend *recommend.Service
	reconcile *reconcile.Service
}

func NewServer(cfg config.Config, db *pgxpool.Pool, log *slog.Logger) *Server {
	marketSvc := marketdata.NewService(db)
	ledgerSvc := ledger.NewService(db)
	return &Server{
		cfg:       cfg,
		db:        db,
		log:       log,
		auth:      auth.NewService(db, []byte(cfg.AppSecret), cfg.EnableRegistration),
		ledger:    ledgerSvc,
		market:    marketSvc,
		recommend: recommend.NewService(db, ledgerSvc, marketSvc),
		reconcile: reconcile.NewService(db, ledgerSvc),
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(s.logRequest)

	r.Get("/healthz", s.handleHealth)
	r.Get("/version", s.handleVersion)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", s.handleHealth)
		r.Get("/version", s.handleVersion)
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Use(s.idempotency)
			r.Get("/me", s.handleMe)
			r.Get("/settings", s.requireScopes("settings:read")(s.handleGetSettings))
			r.Put("/settings", s.requireScopes("settings:write")(s.handlePutSettings))
			r.Get("/strategies", s.handleStrategies)

			r.Get("/api-keys", s.handleListAPIKeys)
			r.Post("/api-keys", s.handleCreateAPIKey)
			r.Post("/api-keys/{id}/revoke", s.handleRevokeAPIKey)
			r.Post("/api-keys/{id}/rotate", s.handleRotateAPIKey)

			r.Get("/assets", s.requireScopes("market:read")(s.handleListAssets))
			r.Get("/assets/{id}", s.requireScopes("market:read")(s.handleGetAsset))
			r.Get("/market/bars", s.requireScopes("market:read")(s.handleMarketBars))
			r.Get("/market/corporate-actions", s.requireScopes("market:read")(s.handleCorporateActions))
			r.Get("/market/calendar", s.requireScopes("market:read")(s.handleCalendar))
			r.Get("/market/freshness", s.requireScopes("market:read")(s.handleFreshness))

			r.Post("/ledger/events", s.requireScopes("ledger:write")(s.handleCreateLedgerEvent))
			r.Get("/ledger/events", s.requireScopes("ledger:read")(s.handleListLedgerEvents))
			r.Get("/ledger/events/{id}", s.requireScopes("ledger:read")(s.handleGetLedgerEvent))
			r.Post("/ledger/events/{id}/void", s.requireScopes("ledger:write")(s.handleVoidLedgerEvent))
			r.Get("/portfolio", s.requireScopes("ledger:read")(s.handlePortfolio))
			r.Get("/portfolio/lots", s.requireScopes("ledger:read")(s.handleLots))

			r.Post("/recommendations/runs", s.requireScopes("recommendations:run")(s.handleCreateRecommendationRun))
			r.Get("/recommendations/runs", s.requireScopes("recommendations:read")(s.handleListRecommendationRuns))
			r.Get("/recommendations/runs/{id}", s.requireScopes("recommendations:read")(s.handleGetRecommendationRun))
			r.Patch("/recommendations/items/{id}", s.requireScopes("recommendations:read")(s.handlePatchRecommendationItem))

			r.Post("/reconciliation/broker-snapshots", s.requireScopes("reconciliation:write")(s.handleCreateBrokerSnapshot))
			r.Get("/reconciliation/broker-snapshots", s.requireScopes("reconciliation:write")(s.handleListBrokerSnapshots))
			r.Post("/reconciliation/runs", s.requireScopes("reconciliation:write")(s.handleCreateReconciliationRun))
			r.Get("/reconciliation/runs/{id}", s.requireScopes("reconciliation:write")(s.handleGetReconciliationRun))
			r.Post("/reconciliation/diffs/{id}/resolve", s.requireScopes("reconciliation:write")(s.handleResolveReconciliationDiff))
		})
	})
	return r
}

func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "internal", "資料庫連線失敗")
		return
	}
	version, dirty, err := platform.MigrationVersion(ctx, s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "internal", "migration 狀態讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"db":                "ok",
		"migration_version": version,
		"migration_dirty":   dirty,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.cfg.Version})
}
