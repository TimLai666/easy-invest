package api

import (
	"errors"
	"net/http"

	"github.com/tingz/easy-invest/internal/auth"
)

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.auth.AuthenticateRequest(r.Context(), r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "請先登入或提供有效 API key")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func currentPrincipal(r *http.Request) (auth.Principal, bool) {
	return auth.PrincipalFromContext(r.Context())
}

func (s *Server) requireScopes(scopes ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := currentPrincipal(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized", "請先登入或提供有效 API key")
				return
			}
			for _, scope := range scopes {
				if !principal.HasScope(scope) {
					writeError(w, http.StatusForbidden, "forbidden", "API key 權限不足")
					return
				}
			}
			next(w, r)
		}
	}
}

func statusForAuthError(err error) (int, string, string) {
	switch {
	case errors.Is(err, auth.ErrRegistrationDisabled):
		return http.StatusForbidden, "forbidden", "目前不開放註冊"
	case errors.Is(err, auth.ErrInvalidCredential):
		return http.StatusUnauthorized, "unauthorized", "帳號或密碼錯誤"
	case errors.Is(err, auth.ErrForbidden):
		return http.StatusForbidden, "forbidden", "此操作只能使用登入 session"
	default:
		return http.StatusInternalServerError, "internal", "系統發生錯誤"
	}
}
