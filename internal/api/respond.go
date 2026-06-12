package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

type ErrorDetail struct {
	Field string `json:"field,omitempty"`
	Issue string `json:"issue"`
}

type ErrorResponse struct {
	Error struct {
		Code    string        `json:"code"`
		Message string        `json:"message"`
		Details []ErrorDetail `json:"details,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, status int, code, message string, details ...ErrorDetail) {
	var resp ErrorResponse
	resp.Error.Code = code
	resp.Error.Message = message
	resp.Error.Details = details
	writeJSON(w, status, resp)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("body must contain a single JSON value")
	}
	return nil
}
