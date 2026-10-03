package httpserver

import (
	"encoding/json"
	"net/http"
)

// errorBody is the one error shape every JSON endpoint returns, so
// the frontend branches on a stable machine-readable code rather than on
// message text.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Machine-readable error codes. Add to this list rather than inventing
// codes at call sites — the frontend switches on these.
const (
	CodeInvalidURL    = "INVALID_URL"
	CodeInvalidAlias  = "INVALID_ALIAS"
	CodeAliasTaken    = "ALIAS_TAKEN"
	CodeBadRequest    = "BAD_REQUEST"
	CodeNotFound      = "NOT_FOUND"
	CodeInternalError = "INTERNAL"
)

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{
		Error: errorDetail{Code: code, Message: message},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
