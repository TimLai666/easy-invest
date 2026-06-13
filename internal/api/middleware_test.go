package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tingz/easy-invest/internal/auth"
)

func TestRequireScopesRequiresEveryScope(t *testing.T) {
	server := &Server{}
	handler := server.requireScopes("recommendations:read", "recommendations:run")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/recommendations/items/item-1", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
		UserID: "user-1",
		Via:    "api_key",
		Scopes: []string{"recommendations:read"},
	}))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireScopesAllowsSession(t *testing.T) {
	server := &Server{}
	handler := server.requireScopes("reconciliation:read")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconciliation/broker-snapshots", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
		UserID: "user-1",
		Via:    "session",
	}))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
