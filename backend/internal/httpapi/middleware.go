package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

type requestIDKey struct{}

// RequestIDFrom returns the request ID set by RequestID.
func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey{}).(string)
	return s
}

// RequestID sets X-Request-Id (reusing a valid incoming one).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.Must(uuid.NewV7()).String()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	buf    *bytes.Buffer
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if s.buf != nil {
		s.buf.Write(b)
	}
	return s.ResponseWriter.Write(b)
}

// AccessLog logs method, path, status and duration. Never bodies, query strings or headers
// (Principle VII, FR-027).
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		slog.InfoContext(r.Context(), "http",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds(), "request_id", RequestIDFrom(r.Context()))
	})
}

// Idempotency replays the stored response for a repeated Idempotency-Key from the same actor,
// and rejects a reused key with a different body (FR-015).
func Idempotency(s *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if r.Method != http.MethodPost || key == "" {
				next.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
			if err != nil {
				WriteError(w, r, Errf(http.StatusBadRequest, gen.InvalidRequest, "body too large"))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
			hash := hex.EncodeToString(sum[:])
			actor := PrincipalFrom(r.Context()).Subject

			prev, err := s.GetIdempotency(r.Context(), db.GetIdempotencyParams{ActorID: actor, Key: key})
			switch {
			case err == nil && prev.RequestHash == hash:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(int(prev.StatusCode))
				_, _ = w.Write(prev.ResponseBody)
				return
			case err == nil:
				WriteError(w, r, Errf(http.StatusConflict, gen.IdempotencyConflict, "Idempotency-Key reused with a different request"))
				return
			case !store.IsNotFound(err):
				WriteError(w, r, err)
				return
			}

			rec := &statusRecorder{ResponseWriter: w, buf: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)
			if rec.status > 0 && rec.status < 500 {
				if err := s.PutIdempotency(r.Context(), db.PutIdempotencyParams{
					Key: key, ActorID: actor, Method: r.Method, Path: r.URL.Path,
					RequestHash: hash, StatusCode: int32(rec.status), ResponseBody: rec.buf.Bytes(),
				}); err != nil {
					slog.WarnContext(r.Context(), "store idempotency key", "err", err)
				}
			}
		})
	}
}

// ETag computes a weak ETag for a versioned resource.
func ETag(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = fmt.Fprint(h, p, "|")
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
}

// CheckIfMatch returns 412 when If-Match is present and differs from current.
func CheckIfMatch(r *http.Request, current string) error {
	im := strings.TrimSpace(r.Header.Get("If-Match"))
	if im == "" || im == "*" || im == current {
		return nil
	}
	return Errf(http.StatusPreconditionFailed, gen.PreconditionFailed, "resource changed; reload and retry")
}
