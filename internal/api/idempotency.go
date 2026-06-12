package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/tingz/easy-invest/internal/auth"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(body)
	return r.ResponseWriter.Write(body)
}

func (s *Server) idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		var status int
		var responseText string
		err := s.db.QueryRow(r.Context(), `
			SELECT status_code, response::text
			FROM idempotency_keys
			WHERE user_id = $1 AND idem_key = $2 AND method = $3 AND path = $4
		`, principal.UserID, key, r.Method, r.URL.Path).Scan(&status, &responseText)
		if err == nil {
			w.Header().Set("Idempotency-Replay", "true")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(responseText))
			return
		}
		if err != nil && err != pgx.ErrNoRows && err != sql.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "internal", "冪等狀態讀取失敗")
			return
		}

		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		if recorder.status >= 500 || recorder.body.Len() == 0 || !json.Valid(recorder.body.Bytes()) {
			return
		}
		_, _ = s.db.Exec(r.Context(), `
			INSERT INTO idempotency_keys (user_id, idem_key, method, path, status_code, response)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb)
			ON CONFLICT (user_id, idem_key, method, path) DO NOTHING
		`, principal.UserID, key, r.Method, r.URL.Path, recorder.status, recorder.body.String())
	})
}
