// Package adminapi implements the Admin HTTP API.
package adminapi

import (
	"encoding/json"
	"net/http"
)

// APIError is the wire format for 4xx/5xx responses.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error APIError `json:"error"`
}

// writeError emits `{"error":{"code":...,"message":...}}` with status.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: APIError{Code: code, Message: msg}})
}

// writeJSON emits v as JSON with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
