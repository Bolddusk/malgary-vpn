package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"malgary/backend/internal/httpapi/gen"
)

// APIError is an error with a fixed problem code (contracts/backend-api.md).
type APIError struct {
	Status int
	Code   gen.ProblemCode
	Detail string
}

func (e *APIError) Error() string { return string(e.Code) + ": " + e.Detail }

// Errf builds an APIError.
func Errf(status int, code gen.ProblemCode, detail string) *APIError {
	return &APIError{Status: status, Code: code, Detail: detail}
}

// Common errors.
var (
	ErrNotFound     = Errf(http.StatusNotFound, gen.NotFound, "not found")
	ErrUnauthorized = Errf(http.StatusUnauthorized, gen.Unauthorized, "missing or invalid token")
)

// WriteError writes err as application/problem+json. Unknown errors become 500 without
// leaking internals to the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if !errors.As(err, &ae) {
		slog.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path)
		ae = Errf(http.StatusInternalServerError, gen.Internal, "internal error")
	}
	p := gen.Problem{
		Type:   "https://api.vpn.malgary.com/problems/" + string(ae.Code),
		Title:  http.StatusText(ae.Status),
		Status: ae.Status,
		Code:   ae.Code,
	}
	if ae.Detail != "" {
		d := ae.Detail
		p.Detail = &d
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(ae.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteJSON writes v with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON decodes the request body strictly: unknown fields are rejected.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errf(http.StatusBadRequest, gen.InvalidRequest, err.Error())
	}
	return nil
}
