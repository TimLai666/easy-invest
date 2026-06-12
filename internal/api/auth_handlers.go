package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tingz/easy-invest/internal/auth"
)

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	user, err := s.auth.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		status, code, msg := statusForAuthError(err)
		if !errors.Is(err, auth.ErrRegistrationDisabled) && !errors.Is(err, auth.ErrInvalidCredential) {
			msg = err.Error()
			code = "validation_failed"
			status = http.StatusBadRequest
		}
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	user, token, err := s.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		status, code, msg := statusForAuthError(err)
		writeError(w, status, code, msg)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
	})
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeNoContent(w)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	principal, _ := currentPrincipal(r)
	user, err := s.auth.CurrentUser(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "使用者資料讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "auth_via": principal.Via, "scopes": principal.Scopes})
}

type createAPIKeyRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, _ := currentPrincipal(r)
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "JSON 格式錯誤")
		return
	}
	if req.Name == "" || len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "validation_failed", "name 與 scopes 必填")
		return
	}
	key, err := s.auth.CreateAPIKey(r.Context(), principal, req.Name, req.Description, req.Scopes, req.ExpiresAt)
	if err != nil {
		status, code, msg := statusForAuthError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	principal, _ := currentPrincipal(r)
	if principal.Via != "session" {
		writeError(w, http.StatusForbidden, "forbidden", "API key 管理只能使用登入 session")
		return
	}
	keys, err := s.auth.ListAPIKeys(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "API key 讀取失敗")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, _ := currentPrincipal(r)
	err := s.auth.RevokeAPIKey(r.Context(), principal, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "找不到 API key")
		return
	}
	if err != nil {
		status, code, msg := statusForAuthError(err)
		writeError(w, status, code, msg)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, _ := currentPrincipal(r)
	key, err := s.auth.RotateAPIKey(r.Context(), principal, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "找不到 API key")
		return
	}
	if err != nil {
		status, code, msg := statusForAuthError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}
